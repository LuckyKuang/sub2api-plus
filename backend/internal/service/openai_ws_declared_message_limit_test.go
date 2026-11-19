//go:build unit || !integration

package service

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/config"
	coderws "github.com/coder/websocket"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestParseOpenAIWSDeclaredMaxMessageBytes(t *testing.T) {
	tests := []struct {
		name    string
		header  string
		set     bool
		want    int
		wantOK  bool
		comment string
	}{
		{name: "absent", set: false},
		{name: "empty", header: "", set: true},
		{name: "whitespace", header: "   ", set: true},
		{name: "padded", header: " 1048576 ", set: true, want: 1048576, wantOK: true},
		{name: "zero", header: "0", set: true},
		{name: "negative", header: "-1", set: true},
		{name: "non numeric", header: "1mb", set: true},
		{name: "float", header: "1.5", set: true},
		{name: "overflow bound", header: "67108865", set: true},
		{name: "at bound", header: "67108864", set: true, want: 67108864, wantOK: true},
		{name: "too long", header: "00000000000000001", set: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			headers := http.Header{}
			if tt.set {
				headers.Set(openAIWSMaxMessageBytesHeader, tt.header)
			}
			got, ok := parseOpenAIWSDeclaredMaxMessageBytes(headers)
			require.Equal(t, tt.wantOK, ok)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestParseOpenAIWSDeclaredMaxMessageBytesNilHeaders(t *testing.T) {
	got, ok := parseOpenAIWSDeclaredMaxMessageBytes(nil)
	require.False(t, ok)
	require.Zero(t, got)
}

func TestOpenAIWSDeclaredLimitCacheExpiresAndForgets(t *testing.T) {
	cache := newOpenAIWSDeclaredLimitCache()
	now := time.Unix(1_700_000_000, 0)
	cache.now = func() time.Time { return now }

	cache.remember("42", 1024)
	limit, ok := cache.lookup("42")
	require.True(t, ok)
	require.Equal(t, 1024, limit)

	now = now.Add(openAIWSDeclaredLimitCacheTTL - time.Second)
	limit, ok = cache.lookup("42")
	require.True(t, ok)
	require.Equal(t, 1024, limit)

	now = now.Add(2 * time.Second)
	_, ok = cache.lookup("42")
	require.False(t, ok)

	cache.remember("42", 2048)
	cache.forget("42")
	_, ok = cache.lookup("42")
	require.False(t, ok)
}

func TestOpenAIWSDeclaredLimitCacheBounded(t *testing.T) {
	cache := newOpenAIWSDeclaredLimitCache()
	cache.now = func() time.Time { return time.Unix(1_700_000_000, 0) }
	for i := 0; i < openAIWSDeclaredLimitEntries+32; i++ {
		cache.remember(strconv.Itoa(i), 1024+i)
	}
	cache.mu.Lock()
	size := len(cache.entries)
	cache.mu.Unlock()
	require.LessOrEqual(t, size, openAIWSDeclaredLimitEntries)
}

func TestRememberOpenAIWSDeclaredMaxMessageBytesIgnoresMissingHeader(t *testing.T) {
	openAIWSDeclaredLimits.reset()
	t.Cleanup(openAIWSDeclaredLimits.reset)

	rememberOpenAIWSDeclaredMaxMessageBytes(7, http.Header{})
	_, ok := openAIWSDeclaredLimits.lookup("7")
	require.False(t, ok)

	headers := http.Header{}
	headers.Set(openAIWSMaxMessageBytesHeader, "4096")
	rememberOpenAIWSDeclaredMaxMessageBytes(7, headers)
	limit, ok := openAIWSDeclaredLimits.lookup("7")
	require.True(t, ok)
	require.Equal(t, 4096, limit)

	// An account without a declaration keeps no entry.
	rememberOpenAIWSDeclaredMaxMessageBytes(0, headers)
	_, ok = openAIWSDeclaredLimits.lookup("0")
	require.False(t, ok)
}

func TestOpenAIWSDeclaredLimitBlocksPayload(t *testing.T) {
	openAIWSDeclaredLimits.reset()
	t.Cleanup(openAIWSDeclaredLimits.reset)

	svc := &OpenAIGatewayService{}
	account := &Account{ID: 11}

	require.False(t, svc.openAIWSDeclaredLimitBlocksPayload(account, 1<<30), "no declaration must never block")

	headers := http.Header{}
	headers.Set(openAIWSMaxMessageBytesHeader, "1024")
	rememberOpenAIWSDeclaredMaxMessageBytes(account.ID, headers)

	require.False(t, svc.openAIWSDeclaredLimitBlocksPayload(account, 1024), "the declared size itself fits")
	require.True(t, svc.openAIWSDeclaredLimitBlocksPayload(account, 1025))
}

func TestCheckOpenAIWSDeclaredMessageLimitBuildsFallbackError(t *testing.T) {
	openAIWSDeclaredLimits.reset()
	t.Cleanup(openAIWSDeclaredLimits.reset)

	svc := &OpenAIGatewayService{}
	account := &Account{ID: 23}
	lease := &openAIWSConnLease{conn: &openAIWSConn{handshakeHeaders: http.Header{
		openAIWSMaxMessageBytesHeader: []string{"2048"},
	}}}

	require.NoError(t, svc.checkOpenAIWSDeclaredMessageLimit(account, lease, 2048))

	err := svc.checkOpenAIWSDeclaredMessageLimit(account, lease, 2049)
	require.Error(t, err)

	var fallbackErr *openAIWSFallbackError
	require.True(t, errors.As(err, &fallbackErr))
	require.Equal(t, openAIWSReasonRequestTooLarge, fallbackErr.Reason)
	require.False(t, fallbackErr.RequestSubmitted, "nothing was written upstream")

	var limitErr *openAIWSDeclaredLimitExceededError
	require.True(t, errors.As(err, &limitErr))
	require.Equal(t, 2048, limitErr.limit)
	require.Equal(t, 2049, limitErr.length)
	require.Equal(t, 2048, openAIWSDeclaredLimitFromError(err))
	require.True(t, isOpenAIWSDeclaredLimitExceeded(err))

	// The declaration is remembered for the account so the next attempt can skip
	// the socket entirely.
	limit, ok := svc.openAIWSDeclaredMessageLimitForAccount(account)
	require.True(t, ok)
	require.Equal(t, 2048, limit)
	require.True(t, svc.openAIWSDeclaredLimitBlocksPayload(account, 4096))
}

func TestCheckOpenAIWSDeclaredMessageLimitWithoutDeclaration(t *testing.T) {
	openAIWSDeclaredLimits.reset()
	t.Cleanup(openAIWSDeclaredLimits.reset)

	svc := &OpenAIGatewayService{}
	account := &Account{ID: 29}
	lease := &openAIWSConnLease{conn: &openAIWSConn{}}

	require.NoError(t, svc.checkOpenAIWSDeclaredMessageLimit(account, lease, 64<<20))
	require.NoError(t, svc.checkOpenAIWSDeclaredMessageLimit(account, nil, 64<<20))
	_, ok := svc.openAIWSDeclaredMessageLimitForAccount(account)
	require.False(t, ok)
}

func TestIsOpenAIWSDeclaredLimitFallbackReason(t *testing.T) {
	require.True(t, isOpenAIWSDeclaredLimitFallbackReason(openAIWSReasonRequestTooLarge))
	require.True(t, isOpenAIWSDeclaredLimitFallbackReason(" prewarm_message_too_big "))
	require.False(t, isOpenAIWSDeclaredLimitFallbackReason("upgrade_required"))
	require.False(t, isOpenAIWSDeclaredLimitFallbackReason(""))
}

func TestOpenAIWSDeclaredLimitRejectsPassthroughWrites(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, later := range []bool{false, true} {
		t.Run(map[bool]string{false: "first turn", true: "later turn"}[later], func(t *testing.T) {
			const accountID = 10231
			key := strconv.Itoa(accountID)
			openAIWSDeclaredLimits.forget(key)
			t.Cleanup(func() { openAIWSDeclaredLimits.forget(key) })

			cfg := &config.Config{}
			cfg.Gateway.OpenAIWS.Enabled = true
			cfg.Gateway.OpenAIWS.APIKeyEnabled = true
			cfg.Gateway.OpenAIWS.ResponsesWebsocketsV2 = true
			cfg.Gateway.OpenAIWS.ModeRouterV2Enabled = true
			upstream := newStagedPassthroughConn()
			dialer := &openAIWSStagedCaptureDialer{
				conn:      upstream,
				handshake: http.Header{openAIWSMaxMessageBytesHeader: []string{"1024"}},
			}
			svc := &OpenAIGatewayService{
				cfg: cfg, cache: &stubGatewayCache{}, openaiWSResolver: NewOpenAIWSProtocolResolver(cfg),
				toolCorrector: NewCodexToolCorrector(), openaiWSPassthroughDialer: dialer,
			}
			account := &Account{
				ID: accountID, Platform: PlatformOpenAI, Type: AccountTypeAPIKey,
				Status: StatusActive, Schedulable: true, Concurrency: 1,
				Credentials: map[string]any{"api_key": "sk-test"},
				Extra:       map[string]any{"openai_apikey_responses_websockets_v2_mode": OpenAIWSIngressModePassthrough},
			}
			serverErr := make(chan error, 1)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				conn, err := coderws.Accept(w, r, nil)
				if err != nil {
					serverErr <- err
					return
				}
				defer func() { _ = conn.CloseNow() }()
				ginCtx, _ := gin.CreateTestContext(httptest.NewRecorder())
				ginCtx.Request = r
				_, first, err := conn.Read(r.Context())
				if err == nil {
					err = svc.ProxyResponsesWebSocketFromClient(r.Context(), ginCtx, conn, account, "sk-test", first, nil)
				}
				var closeErr *OpenAIWSClientCloseError
				if errors.As(err, &closeErr) {
					_ = conn.Close(closeErr.StatusCode(), closeErr.Reason())
				}
				serverErr <- err
			}))
			defer server.Close()
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			conn, _, err := coderws.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http"), nil)
			require.NoError(t, err)
			defer func() { _ = conn.CloseNow() }()
			if later {
				require.NoError(t, conn.Write(ctx, coderws.MessageText, []byte(`{"type":"response.create","model":"gpt-5.1","input":"ok"}`)))
				select {
				case <-upstream.writes:
				case <-ctx.Done():
					t.Fatal("first turn did not reach the upstream socket")
				}
				upstream.Send(`{"type":"response.completed","response":{"id":"resp_first","model":"gpt-5.1","usage":{"input_tokens":2,"output_tokens":3}}}`)
				_, _, err = conn.Read(ctx)
				require.NoError(t, err)
			}

			oversized := `{"type":"response.create","model":"gpt-5.1","input":"` + strings.Repeat("x", 1024) + `"}`
			require.NoError(t, conn.Write(ctx, coderws.MessageText, []byte(oversized)))
			_, _, err = conn.Read(ctx)
			require.Equal(t, coderws.StatusMessageTooBig, coderws.CloseStatus(err))
			select {
			case err := <-serverErr:
				var sizeErr *openAIWSDeclaredLimitExceededError
				require.ErrorAs(t, err, &sizeErr)
			case <-ctx.Done():
				t.Fatal("gateway did not finish the oversized turn")
			}
			require.Empty(t, upstream.writes)
			limit, ok := svc.openAIWSDeclaredMessageLimitForAccount(account)
			require.True(t, ok)
			require.Equal(t, 1024, limit)
		})
	}
}

func TestOpenAIWSDeclaredLimitSkipsOversizedGeneratePrewarm(t *testing.T) {
	cfg := &config.Config{}
	cfg.Gateway.OpenAIWS.PrewarmGenerateEnabled = true
	svc := &OpenAIGatewayService{cfg: cfg}
	account := &Account{ID: 10232}
	upstream := &openAIWSCaptureConn{}
	payload := map[string]any{"type": "response.create", "model": "gpt-5.1"}
	headers := http.Header{openAIWSMaxMessageBytesHeader: []string{"44"}}
	lease := &openAIWSConnLease{conn: newOpenAIWSConn("prewarm_limit", account.ID, upstream, headers)}
	decision := OpenAIWSProtocolDecision{Transport: OpenAIUpstreamTransportResponsesWebsocketV2}

	require.NoError(t, svc.performOpenAIWSGeneratePrewarm(t.Context(), lease, decision, payload, "", payload, account, nil, 0))
	require.Empty(t, upstream.writes)
}

func TestOpenAIWSDeclaredLimitFallsBackBeforePrewarmAndTurnState(t *testing.T) {
	cfg := &config.Config{}
	cfg.Gateway.OpenAIWS.Enabled = true
	cfg.Gateway.OpenAIWS.APIKeyEnabled = true
	cfg.Gateway.OpenAIWS.ResponsesWebsocketsV2 = true
	cfg.Gateway.OpenAIWS.PrewarmGenerateEnabled = true
	cfg.Gateway.OpenAIWS.MaxConnsPerAccount = 1
	cfg.Gateway.OpenAIWS.MaxIdlePerAccount = 1
	wsConn := &openAIWSCaptureConn{}
	dialer := &openAIWSCaptureDialer{conn: wsConn, handshake: http.Header{
		openAIWSMaxMessageBytesHeader:                    {"128"},
		http.CanonicalHeaderKey(openAIWSTurnStateHeader): {"unused-websocket-state"},
	}}
	pool := newOpenAIWSConnPool(cfg)
	t.Cleanup(pool.Close)
	pool.setClientDialerForTest(dialer)
	httpUpstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"application/json"}},
		Body: io.NopCloser(strings.NewReader(`{"id":"resp_http","model":"gpt-5.1","output":[],"usage":{"input_tokens":2,"output_tokens":3}}`)),
	}}
	svc := &OpenAIGatewayService{
		cfg: cfg, cache: &stubGatewayCache{}, httpUpstream: httpUpstream,
		openaiWSResolver: NewOpenAIWSProtocolResolver(cfg), toolCorrector: NewCodexToolCorrector(), openaiWSPool: pool,
	}
	account := &Account{
		ID: 10233, Platform: PlatformOpenAI, Type: AccountTypeAPIKey,
		Status: StatusActive, Schedulable: true, Concurrency: 1,
		Credentials: map[string]any{"api_key": "sk-test"},
		Extra:       map[string]any{"responses_websockets_v2_enabled": true},
	}
	t.Cleanup(func() { openAIWSDeclaredLimits.forget("10233") })
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	body := `{"model":"gpt-5.1","stream":false,"input":"` + strings.Repeat("x", 128) + `"}`

	result, err := svc.Forward(t.Context(), c, account, []byte(body))
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, 1, dialer.DialCount())
	require.Empty(t, wsConn.writes)
	require.Len(t, httpUpstream.requests, 1)
	require.Empty(t, rec.Header().Get(openAIWSTurnStateHeader))
	require.Contains(t, rec.Body.String(), "resp_http")
}
