//go:build unit || !integration

package handler

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/config"
	"github.com/LuckyKuang/sub2api-plus/internal/pkg/ctxkey"
	"github.com/LuckyKuang/sub2api-plus/internal/pkg/openai_compat"
	"github.com/LuckyKuang/sub2api-plus/internal/pkg/tlsfingerprint"
	"github.com/LuckyKuang/sub2api-plus/internal/server/middleware"
	"github.com/LuckyKuang/sub2api-plus/internal/service"
	coderws "github.com/coder/websocket"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type mixedProtocolAccountRepo struct {
	service.AccountRepository
	accounts []service.Account
}

func (r *mixedProtocolAccountRepo) ListSchedulableByPlatform(_ context.Context, platform string) ([]service.Account, error) {
	var found []service.Account
	for _, account := range r.accounts {
		if account.Platform == platform {
			found = append(found, account)
		}
	}
	return found, nil
}
func (r *mixedProtocolAccountRepo) ListSchedulableByGroupIDAndPlatform(ctx context.Context, _ int64, platform string) ([]service.Account, error) {
	return r.ListSchedulableByPlatform(ctx, platform)
}
func (r *mixedProtocolAccountRepo) GetByID(_ context.Context, id int64) (*service.Account, error) {
	for i := range r.accounts {
		if r.accounts[i].ID == id {
			return &r.accounts[i], nil
		}
	}
	return nil, service.ErrAccountNotFound
}
func (r *mixedProtocolAccountRepo) ListModelAvailabilityCandidates(ctx context.Context, _ *int64, platforms []string, _ bool) ([]service.Account, error) {
	var found []service.Account
	for _, platform := range platforms {
		accounts, err := r.ListSchedulableByPlatform(ctx, platform)
		if err != nil {
			return nil, err
		}
		found = append(found, accounts...)
	}
	return found, nil
}

type mixedProtocolUsageRepo struct{ service.UsageLogRepository }

func (mixedProtocolUsageRepo) Create(context.Context, *service.UsageLog) (bool, error) {
	return true, nil
}

type mixedProtocolHTTP struct{ client *http.Client }

// Preserve the resolver's official-format candidates without relying on the
// unit-tagged package TestMain or selecting identity headers in the fixture.
type mixedProtocolIDStore struct{ values sync.Map }

func (s *mixedProtocolIDStore) ResolveOutboundID(_ context.Context, namespace, digest, candidate string) (string, error) {
	value, _ := s.values.LoadOrStore(namespace+":"+digest, candidate)
	resolved, ok := value.(string)
	if !ok {
		return "", fmt.Errorf("unexpected outbound identity mapping type %T", value)
	}
	return resolved, nil
}

func (u mixedProtocolHTTP) Do(req *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
	return u.client.Do(req)
}
func (u mixedProtocolHTTP) DoWithTLS(req *http.Request, proxy string, id int64, limit int, _ *tlsfingerprint.Profile) (*http.Response, error) {
	return u.Do(req, proxy, id, limit)
}

func TestGatewayGlobalCapacityCombinesHTTPResponsesWSAndVoiceAndRejectsLive(t *testing.T) {
	// One user's C=3 must count real HTTP, active Responses WS turns, and Voice
	// together, even across keys and platforms. Live and a fourth HTTP request
	// cannot bypass this pool. Losing only Voice's owner must cancel its actual
	// provider request without cancelling the other two admitted executions.
	var providerCalls atomic.Int32
	wsStarted := make(chan struct{})
	wsGate := make(chan struct{})
	var releaseWSOnce sync.Once
	releaseWS := func() { releaseWSOnce.Do(func() { close(wsGate) }) }
	wsUpstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := coderws.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer func() { _ = conn.CloseNow() }()
		ctx, cancel := context.WithTimeout(r.Context(), 8*time.Second)
		defer cancel()
		if _, _, err = conn.Read(ctx); err != nil {
			return
		}
		providerCalls.Add(1)
		close(wsStarted)
		select {
		case <-wsGate:
		case <-ctx.Done():
			return
		}
		if err = conn.Write(ctx, coderws.MessageText, []byte(`{"type":"response.completed","response":{"id":"resp_mixed_capacity","model":"gpt-5.1","usage":{"input_tokens":1,"output_tokens":1}}}`)); err != nil {
			return
		}
		_, _, _ = conn.Read(ctx)
	}))
	defer wsUpstream.Close()
	defer releaseWS()
	h := newOpenAIWSGlobalConcurrencyHarness(t, wsUpstream.URL, 3)
	defer func() { _ = h.clientConn.CloseNow() }()
	writeWSGlobalConcurrencyTurn(t, h.clientConn)
	select {
	case <-wsStarted:
	case <-time.After(3 * time.Second):
		t.Fatal("Responses WS turn did not reach provider")
	}

	chatStarted := make(chan struct{})
	voiceStarted := make(chan struct{})
	voiceStopped := make(chan struct{})
	voiceCleanup := make(chan struct{})
	chatGate := make(chan struct{})
	var releaseChatOnce sync.Once
	var releaseVoiceOnce sync.Once
	var chatStartedOnce sync.Once
	var voiceStartedOnce sync.Once
	releaseChat := func() { releaseChatOnce.Do(func() { close(chatGate) }) }
	releaseVoice := func() { releaseVoiceOnce.Do(func() { close(voiceCleanup) }) }
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		providerCalls.Add(1)
		switch r.Header.Get("Authorization") {
		case "Bearer mixed-chat":
			chatStartedOnce.Do(func() { close(chatStarted) })
			select {
			case <-chatGate:
			case <-r.Context().Done():
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"id":"mixed_http","object":"chat.completion","model":"gpt-5.1","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`)
		case "Bearer mixed-voice":
			voiceStartedOnce.Do(func() { close(voiceStarted) })
			select {
			case <-r.Context().Done():
				close(voiceStopped)
			case <-voiceCleanup:
				return
			}
		default:
			http.Error(w, "unexpected credential", http.StatusInternalServerError)
		}
	}))
	defer upstream.Close()
	defer releaseChat()
	defer releaseVoice()
	cfg := &config.Config{RunMode: config.RunModeSimple}
	cfg.Default.RateMultiplier = 1
	cfg.Security.URLAllowlist.AllowInsecureHTTP = true
	cfg.Gateway.MaxBodySize = 1 << 20
	cfg.Gateway.TextMaxBodySize = 1 << 20
	chatGroup := &service.Group{ID: 4301, Platform: service.PlatformOpenAI, Status: service.StatusActive, Hydrated: true, RateMultiplier: 1, AllowLive: true}
	voiceGroup := &service.Group{ID: 4302, Platform: service.PlatformGrok, Status: service.StatusActive, Hydrated: true, RateMultiplier: 1}
	repo := &mixedProtocolAccountRepo{accounts: []service.Account{
		{ID: 9902, Platform: service.PlatformOpenAI, Type: service.AccountTypeAPIKey, Status: service.StatusActive, Schedulable: true, Concurrency: 10,
			Credentials: map[string]any{"api_key": "mixed-chat", "base_url": upstream.URL}, Extra: map[string]any{openai_compat.ExtraKeyResponsesMode: string(openai_compat.ResponsesSupportModeForceChatCompletions)}, AccountGroups: []service.AccountGroup{{AccountID: 9902, GroupID: chatGroup.ID}}},
		{ID: 9903, Platform: service.PlatformGrok, Type: service.AccountTypeAPIKey, Status: service.StatusActive, Schedulable: true, Concurrency: 10,
			Credentials: map[string]any{"api_key": "mixed-voice", "base_url": upstream.URL}, AccountGroups: []service.AccountGroup{{AccountID: 9903, GroupID: voiceGroup.ID}}},
	}}
	billing := service.NewBillingCacheService(nil, nil, nil, nil, nil, nil, cfg, nil)
	defer billing.Stop()
	gw := service.NewOpenAIGatewayService(repo, mixedProtocolUsageRepo{}, nil, nil, nil, nil, nil, cfg, nil, h.concurrency, service.NewBillingService(cfg, nil), nil, billing, mixedProtocolHTTP{upstream.Client()}, &service.DeferredService{}, nil, nil, nil, nil, nil, nil, nil, nil)
	protocols := NewOpenAIGatewayHandler(gw, h.concurrency, billing, &service.APIKeyService{}, nil, nil, nil, nil, cfg)
	identityIDs := &mixedProtocolIDStore{}
	router := gin.New()
	router.Use(func(c *gin.Context) {
		group, keyID := chatGroup, int64(1802)
		if c.Request.URL.Path == "/tts" {
			group, keyID = voiceGroup, 1803
		}
		if c.GetHeader("Authorization") == "Bearer fourth-key" {
			keyID = 1804
		}
		user := &service.User{ID: wsGlobalConcurrencyUserID, Concurrency: 99, Balance: 100, Status: service.StatusActive}
		c.Set(string(middleware.ContextKeyAPIKey), &service.APIKey{ID: keyID, UserID: user.ID, User: user, GroupID: &group.ID, Group: group})
		c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: user.ID, Concurrency: 99})
		c.Request = c.Request.WithContext(context.WithValue(c.Request.Context(), ctxkey.Group, group))
		c.Request = c.Request.WithContext(service.WithOutboundIDStore(c.Request.Context(), identityIDs))
		c.Next()
	})
	router.POST("/v1/chat/completions", protocols.ChatCompletions)
	router.POST("/tts", func(c *gin.Context) { protocols.GrokVoice(c, "tts") })
	router.POST("/v1/live", protocols.Live)
	gateway := httptest.NewServer(router)
	defer gateway.Close()
	defer releaseChat()
	defer releaseVoice()
	client := gateway.Client()
	client.Timeout = 5 * time.Second
	type outcome struct {
		status int
		body   string
		err    error
	}
	send := func(ctx context.Context, path, body, key string) outcome {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, gateway.URL+path, strings.NewReader(body))
		if err != nil {
			return outcome{err: err}
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+key)
		resp, err := client.Do(req)
		if err != nil {
			return outcome{err: err}
		}
		defer func() { _ = resp.Body.Close() }()
		data, err := io.ReadAll(resp.Body)
		return outcome{resp.StatusCode, string(data), err}
	}
	chatBody := `{"model":"gpt-5.1","messages":[{"role":"user","content":"parent and subagent capacity"}]}`
	chatResult := make(chan outcome, 1)
	go func() { chatResult <- send(context.Background(), "/v1/chat/completions", chatBody, "chat-key") }()
	select {
	case <-chatStarted:
	case result := <-chatResult:
		t.Fatalf("HTTP finished before provider admission: %+v", result)
	case <-time.After(3 * time.Second):
		t.Fatal("HTTP Chat did not reach provider")
	}
	ownersBeforeVoice, err := h.owners.List(context.Background(), wsGlobalConcurrencyUserID)
	require.NoError(t, err)
	require.Len(t, ownersBeforeVoice, 2)
	voiceResult := make(chan outcome, 1)
	go func() {
		voiceResult <- send(context.Background(), "/tts", `{"input":"hello","voice_id":"eve","language":"en"}`, "voice-key")
	}()
	select {
	case <-voiceStarted:
	case result := <-voiceResult:
		t.Fatalf("Voice finished before provider admission: %+v", result)
	case <-time.After(3 * time.Second):
		t.Fatal("Grok Voice did not reach provider")
	}
	count, err := h.pool.GetUserConcurrency(context.Background(), wsGlobalConcurrencyUserID)
	require.NoError(t, err)
	require.Equal(t, 3, count, "HTTP + active WS turn + Voice must share one global user pool")
	live := send(context.Background(), "/v1/live", `{"sdp":"v=0\r\n","session":{"model":"gpt-live-test"}}`, "live-key")
	require.NoError(t, live.err)
	require.Equal(t, http.StatusTooManyRequests, live.status, live.body)
	require.Contains(t, live.body, "Live concurrency limit reached")
	blockedCtx, stopBlocked := context.WithTimeout(context.Background(), time.Second)
	rejectionStarted := time.Now()
	blocked := send(blockedCtx, "/v1/chat/completions", chatBody, "fourth-key")
	stopBlocked()
	require.Less(t, time.Since(rejectionStarted), time.Second)
	require.NoError(t, blocked.err, "full user must receive HTTP 429 instead of waiting for a timeout")
	require.Equal(t, http.StatusTooManyRequests, blocked.status, blocked.body)
	loads, err := h.pool.GetUsersLoadBatch(context.Background(), []service.UserWithConcurrency{{ID: wsGlobalConcurrencyUserID, MaxConcurrency: 3}})
	require.NoError(t, err)
	require.NotNil(t, loads[wsGlobalConcurrencyUserID])
	require.Zero(t, loads[wsGlobalConcurrencyUserID].WaitingCount)
	require.Equal(t, 3, loads[wsGlobalConcurrencyUserID].CurrentConcurrency, "all existing executions remain held during rejection")
	keyCounts, err := h.concurrency.GetAPIKeyConcurrencyBatch(context.Background(), []int64{1801, 1802, 1803, 1804})
	require.NoError(t, err)
	require.Equal(t, 1, keyCounts[1801])
	require.Equal(t, 1, keyCounts[1802])
	require.Equal(t, 1, keyCounts[1803])
	require.Zero(t, keyCounts[1804])
	require.EqualValues(t, 3, providerCalls.Load(), "Live and a fourth HTTP key must not send upstream while all three slots are occupied")
	select {
	case result := <-chatResult:
		t.Fatalf("Chat completed before its explicit provider release: %+v", result)
	default:
	}
	select {
	case <-voiceStopped:
		t.Fatal("Voice stopped before its owner was revoked")
	default:
	}

	ownersAfterVoice, err := h.owners.List(context.Background(), wsGlobalConcurrencyUserID)
	require.NoError(t, err)
	voiceOwner := ""
	for _, candidate := range ownersAfterVoice {
		if candidate != ownersBeforeVoice[0] && candidate != ownersBeforeVoice[1] {
			require.Empty(t, voiceOwner)
			voiceOwner = candidate
		}
	}
	require.NotEmpty(t, voiceOwner, "Voice must hold its own revocable global user owner")
	require.NoError(t, h.owners.Remove(context.Background(), wsGlobalConcurrencyUserID, voiceOwner))
	select {
	case <-voiceStopped:
	case <-time.After(time.Second):
		t.Fatal("lost Voice capacity did not cancel the real provider HTTP request")
	}
	select {
	case result := <-voiceResult:
		require.NoError(t, result.err)
		require.NotEqual(t, http.StatusOK, result.status, result.body)
	case <-time.After(time.Second):
		t.Fatal("Voice handler survived loss of its user execution lease")
	}
	count, err = h.pool.GetUserConcurrency(context.Background(), wsGlobalConcurrencyUserID)
	require.NoError(t, err)
	require.Equal(t, 2, count, "loss of Voice owner must preserve the independent HTTP and WS owners")
	select {
	case result := <-chatResult:
		t.Fatalf("Voice capacity loss cancelled independent Chat: %+v", result)
	default:
	}
	releaseChat()
	chat := <-chatResult
	require.NoError(t, chat.err)
	require.Equal(t, http.StatusOK, chat.status, chat.body)
	releaseWS()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	_, payload, err := h.clientConn.Read(ctx)
	cancel()
	require.NoError(t, err)
	require.Contains(t, string(payload), "response.completed")
	require.Eventually(t, func() bool {
		users, e := h.pool.GetUserConcurrency(context.Background(), wsGlobalConcurrencyUserID)
		wsAccounts, we := h.pool.GetAccountConcurrency(context.Background(), 9901)
		chatAccounts, ce := h.pool.GetAccountConcurrency(context.Background(), 9902)
		voiceAccounts, ae := h.pool.GetAccountConcurrency(context.Background(), 9903)
		return e == nil && we == nil && ce == nil && ae == nil && users == 0 && wsAccounts == 0 && chatAccounts == 0 && voiceAccounts == 0
	}, time.Second, 5*time.Millisecond, fmt.Sprintf("all actual executions must release capacity; provider calls=%d", providerCalls.Load()))
	_ = h.clientConn.CloseNow()
}
