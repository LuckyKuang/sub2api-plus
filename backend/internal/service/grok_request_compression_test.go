//go:build unit

package service

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/klauspost/compress/zstd"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"

	"github.com/LuckyKuang/sub2api-plus/internal/config"
	"github.com/LuckyKuang/sub2api-plus/internal/pkg/tlsfingerprint"
	"github.com/LuckyKuang/sub2api-plus/internal/pkg/xai"
)

func grokCompressionPtrInt64(value int64) *int64 { return &value }

// grokCompressionUpstreamStub routes the capability probe (`/v1/settings`) and
// the sampler send independently so a test can observe both.
type grokCompressionUpstreamStub struct {
	mu sync.Mutex

	settingsStatus int
	settingsBody   string
	settingsErr    error
	settingsCalls  int

	inferenceStatus int
	inferenceBody   string
	inferenceHeader http.Header
	inferenceCalls  int

	requests   []*http.Request
	bodies     [][]byte
	settingsRe []*http.Request
	proxies    []string
}

func (u *grokCompressionUpstreamStub) Do(req *http.Request, proxyURL string, accountID int64, accountConcurrency int) (*http.Response, error) {
	u.mu.Lock()
	defer u.mu.Unlock()

	var body []byte
	if req != nil && req.Body != nil {
		body, _ = io.ReadAll(req.Body)
		_ = req.Body.Close()
		req.Body = io.NopCloser(bytes.NewReader(body))
	}
	u.requests = append(u.requests, req)
	u.bodies = append(u.bodies, append([]byte(nil), body...))
	u.proxies = append(u.proxies, proxyURL)

	isSettings := req != nil && req.URL != nil && strings.HasSuffix(req.URL.Path, "/settings")
	if isSettings {
		u.settingsCalls++
		u.settingsRe = append(u.settingsRe, req)
		if u.settingsErr != nil {
			return nil, u.settingsErr
		}
		status := u.settingsStatus
		if status == 0 {
			status = http.StatusOK
		}
		return &http.Response{
			StatusCode: status,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(u.settingsBody)),
			Request:    req,
		}, nil
	}
	u.inferenceCalls++
	status := u.inferenceStatus
	if status == 0 {
		status = http.StatusOK
	}
	respBody := u.inferenceBody
	if respBody == "" {
		respBody = `{"id":"resp_1","object":"response","model":"grok-4.3","output":[],"usage":{"input_tokens":1,"output_tokens":1,"total_tokens":2}}`
	}
	header := u.inferenceHeader
	if header == nil {
		header = http.Header{"Content-Type": []string{"application/json"}}
	}
	return &http.Response{
		StatusCode: status,
		Header:     header,
		Body:       io.NopCloser(strings.NewReader(respBody)),
		Request:    req,
	}, nil
}

func (u *grokCompressionUpstreamStub) DoWithTLS(req *http.Request, proxyURL string, accountID int64, accountConcurrency int, _ *tlsfingerprint.Profile) (*http.Response, error) {
	return u.Do(req, proxyURL, accountID, accountConcurrency)
}

func (u *grokCompressionUpstreamStub) snapshot() (int, int) {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.settingsCalls, u.inferenceCalls
}

func grokCompressionEnabledConfig() *config.Config {
	cfg := &config.Config{}
	cfg.Gateway.Grok.GrokRequestCompressionEnabled = true
	return cfg
}

func grokCompressionTestAccount(id int64) *Account {
	return &Account{
		ID:          id,
		Name:        "grok-compression",
		Platform:    PlatformGrok,
		Type:        AccountTypeOAuth,
		Concurrency: 1,
		Credentials: map[string]any{
			"access_token":      "access-token",
			"sub":               "sub-owner",
			"email":             "owner@example.test",
			"base_url":          xai.DefaultCLIBaseURL,
			"subscription_tier": "paid",
		},
	}
}

// grokCompressionPaddedJSON returns valid JSON of an exact byte length so the
// 64 KiB boundary is tested on the final serialized body, not on a helper.
func grokCompressionPaddedJSON(t *testing.T, size int) []byte {
	t.Helper()
	const prefix = `{"model":"grok-4.3","input":"`
	const suffix = `"}`
	pad := size - len(prefix) - len(suffix)
	require.GreaterOrEqual(t, pad, 0)
	body := []byte(prefix + strings.Repeat("x", pad) + suffix)
	require.Len(t, body, size)
	return body
}

func decodeGrokCompressionBody(t *testing.T, req *http.Request) []byte {
	t.Helper()
	raw, err := io.ReadAll(req.Body)
	require.NoError(t, err)
	decoded, err := zstd.NewReader(bytes.NewReader(raw))
	require.NoError(t, err)
	defer decoded.Close()
	plain, err := io.ReadAll(decoded)
	require.NoError(t, err)
	return plain
}

func TestGrokRequestCompressionEncodesOnlyWhenCapabilityAdvertised(t *testing.T) {
	resetGrokRequestCompressionCapabilityCacheForTest()
	t.Cleanup(resetGrokRequestCompressionCapabilityCacheForTest)

	upstream := &grokCompressionUpstreamStub{settingsBody: `{"accept_request_encodings":["zstd"]}`}
	svc := &OpenAIGatewayService{cfg: grokCompressionEnabledConfig(), httpUpstream: upstream}
	body := grokCompressionPaddedJSON(t, grokRequestCompressionMinBytes+1024)

	req, err := svc.buildGrokResponsesRequest(context.Background(), nil, grokCompressionTestAccount(11), body, "access-token", "", svc.cfg)
	require.NoError(t, err)
	require.Equal(t, "zstd", req.Header.Get("Content-Encoding"))
	require.Equal(t, "application/json", req.Header.Get("Content-Type"))
	require.Equal(t, "application/json", req.Header.Get("Accept"))
	require.Equal(t, req.ContentLength, int64(len(lenMustRead(t, req))))
	require.Equal(t, body, decodeGrokCompressionBody(t, req))

	// The recorded final JSON must reproduce a replayable plain request.
	replay, err := req.GetBody()
	require.NoError(t, err)
	replayed, err := io.ReadAll(replay)
	require.NoError(t, err)
	require.Equal(t, int64(len(replayed)), req.ContentLength)
	require.NoError(t, RebuildGrokPlainRequest(req))
	require.Empty(t, req.Header.Get("Content-Encoding"))
	require.Equal(t, int64(len(body)), req.ContentLength)
	require.Equal(t, body, decodeGrokPlainBody(t, req))

	settingsCalls, inferenceCalls := upstream.snapshot()
	require.Equal(t, 1, settingsCalls)
	require.Equal(t, 0, inferenceCalls)
	settingsReq := upstream.settingsRe[0]
	require.Equal(t, xai.DefaultCLIBaseURL+"/settings", settingsReq.URL.String())
	require.Equal(t, "Bearer access-token", settingsReq.Header.Get("Authorization"))
	require.Equal(t, xai.CLITokenAuth, settingsReq.Header.Get("X-XAI-Token-Auth"))
	require.Equal(t, "sub-owner", settingsReq.Header.Get("x-userid"))
	require.Equal(t, "owner@example.test", settingsReq.Header.Get("x-email"))
	// A capability lookup never acquires sampler declarations.
	require.Empty(t, settingsReq.Header.Get("x-grok-req-id"))
	require.Empty(t, settingsReq.Header.Get("x-grok-agent-id"))
	require.Empty(t, settingsReq.Header.Get("x-grok-model-override"))
	require.Empty(t, settingsReq.Header.Get("x-grok-session-id"))
	require.Empty(t, settingsReq.Header.Get("x-grok-conv-group-id"))
	require.Empty(t, settingsReq.Header.Get("x-authenticateresponse"))
	// The same-owner identity snapshot still renders the Grok triple.
	require.Equal(t, xai.CLIUserAgent(xai.CLIClientVersion), settingsReq.Header.Get("User-Agent"))
	require.Equal(t, xai.CLIClientIdentifier, settingsReq.Header.Get("x-grok-client-identifier"))
	require.Equal(t, xai.CLIClientVersion, settingsReq.Header.Get("x-grok-client-version"))
	require.Equal(t, xai.CLIClientMode, settingsReq.Header.Get("x-grok-client-mode"))
}

func lenMustRead(t *testing.T, req *http.Request) []byte {
	t.Helper()
	raw, err := io.ReadAll(req.Body)
	require.NoError(t, err)
	req.Body = io.NopCloser(bytes.NewReader(raw))
	return raw
}

func decodeGrokPlainBody(t *testing.T, req *http.Request) []byte {
	t.Helper()
	raw, err := io.ReadAll(req.Body)
	require.NoError(t, err)
	req.Body = io.NopCloser(bytes.NewReader(raw))
	return raw
}

func TestGrokRequestCompressionBoundaryAndKillSwitch(t *testing.T) {
	resetGrokRequestCompressionCapabilityCacheForTest()
	t.Cleanup(resetGrokRequestCompressionCapabilityCacheForTest)

	tests := []struct {
		name           string
		size           int
		enabled        bool
		wantCompressed bool
		wantSettings   int
	}{
		{name: "one byte below threshold stays plain", size: grokRequestCompressionMinBytes - 1, enabled: true, wantCompressed: false, wantSettings: 0},
		{name: "exactly at threshold compresses", size: grokRequestCompressionMinBytes, enabled: true, wantCompressed: true, wantSettings: 1},
		{name: "above threshold compresses", size: grokRequestCompressionMinBytes * 4, enabled: true, wantCompressed: true, wantSettings: 1},
		{name: "kill switch keeps large bodies plain", size: grokRequestCompressionMinBytes * 4, enabled: false, wantCompressed: false, wantSettings: 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			resetGrokRequestCompressionCapabilityCacheForTest()
			upstream := &grokCompressionUpstreamStub{settingsBody: `{"accept_request_encodings":["zstd"]}`}
			cfg := grokCompressionEnabledConfig()
			cfg.Gateway.Grok.GrokRequestCompressionEnabled = tc.enabled
			svc := &OpenAIGatewayService{cfg: cfg, httpUpstream: upstream}
			body := grokCompressionPaddedJSON(t, tc.size)

			req, err := svc.buildGrokResponsesRequest(context.Background(), nil, grokCompressionTestAccount(12), body, "access-token", "", cfg)
			require.NoError(t, err)
			if tc.wantCompressed {
				require.Equal(t, "zstd", req.Header.Get("Content-Encoding"))
				require.Equal(t, body, decodeGrokCompressionBody(t, req))
			} else {
				require.Empty(t, req.Header.Get("Content-Encoding"))
				require.Equal(t, int64(tc.size), req.ContentLength)
				require.Equal(t, body, decodeGrokPlainBody(t, req))
			}
			settingsCalls, _ := upstream.snapshot()
			require.Equal(t, tc.wantSettings, settingsCalls)
		})
	}
}

func TestGrokRequestCompressionUnknownAndWithdrawnCapabilityStaysPlain(t *testing.T) {
	resetGrokRequestCompressionCapabilityCacheForTest()
	t.Cleanup(resetGrokRequestCompressionCapabilityCacheForTest)

	tests := []struct {
		name         string
		status       int
		settingsBody string
		err          error
	}{
		{name: "missing field", settingsBody: `{"announcements":[]}`},
		{name: "withdrawn", settingsBody: `{"accept_request_encodings":[]}`},
		{name: "other encoding only", settingsBody: `{"accept_request_encodings":["br"]}`},
		{name: "unknown shape is not json", settingsBody: `not-json`},
		{name: "unknown shape is an array", settingsBody: `["zstd"]`},
		{name: "non success status", status: http.StatusForbidden, settingsBody: `{"accept_request_encodings":["zstd"]}`},
		{name: "refresh error", err: errors.New("dial failed"), settingsBody: `{"accept_request_encodings":["zstd"]}`},
	}
	for i, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			resetGrokRequestCompressionCapabilityCacheForTest()
			upstream := &grokCompressionUpstreamStub{
				settingsStatus: tc.status,
				settingsBody:   tc.settingsBody,
				settingsErr:    tc.err,
			}
			svc := &OpenAIGatewayService{cfg: grokCompressionEnabledConfig(), httpUpstream: upstream}
			body := grokCompressionPaddedJSON(t, grokRequestCompressionMinBytes*2)

			req, err := svc.buildGrokResponsesRequest(context.Background(), nil, grokCompressionTestAccount(int64(100+i)), body, "access-token", "", svc.cfg)
			require.NoError(t, err)
			require.Empty(t, req.Header.Get("Content-Encoding"))
			require.Equal(t, body, decodeGrokPlainBody(t, req))

			// The negative observation is bounded by the cache TTL, so a second
			// request in the same window does not probe again.
			_, err = svc.buildGrokResponsesRequest(context.Background(), nil, grokCompressionTestAccount(int64(100+i)), body, "access-token", "", svc.cfg)
			require.NoError(t, err)
			settingsCalls, _ := upstream.snapshot()
			require.Equal(t, 1, settingsCalls)
		})
	}
}

func TestGrokRequestCompressionWithdrawalIsObservedAfterCacheExpiry(t *testing.T) {
	resetGrokRequestCompressionCapabilityCacheForTest()
	t.Cleanup(resetGrokRequestCompressionCapabilityCacheForTest)

	upstream := &grokCompressionUpstreamStub{settingsBody: `{"accept_request_encodings":["zstd"]}`}
	svc := &OpenAIGatewayService{cfg: grokCompressionEnabledConfig(), httpUpstream: upstream}
	account := grokCompressionTestAccount(13)
	body := grokCompressionPaddedJSON(t, grokRequestCompressionMinBytes*2)

	req, err := svc.buildGrokResponsesRequest(context.Background(), nil, account, body, "access-token", "", svc.cfg)
	require.NoError(t, err)
	require.Equal(t, "zstd", req.Header.Get("Content-Encoding"))

	// The proxy withdraws the capability; the bounded cache keeps the earlier
	// observation until it expires, then the gateway stops compressing.
	upstream.mu.Lock()
	upstream.settingsBody = `{"accept_request_encodings":[]}`
	upstream.mu.Unlock()

	req, err = svc.buildGrokResponsesRequest(context.Background(), nil, account, body, "access-token", "", svc.cfg)
	require.NoError(t, err)
	require.Equal(t, "zstd", req.Header.Get("Content-Encoding"), "cached advertisement lasts until TTL")

	key := grokRequestCompressionCacheKey{target: "https://cli-chat-proxy.grok.com:443/v1", owner: account.ID}
	grokRequestCompressionCacheStore(key, true, time.Now().Add(-2*grokRequestCompressionCapabilityTTL))

	req, err = svc.buildGrokResponsesRequest(context.Background(), nil, account, body, "access-token", "", svc.cfg)
	require.NoError(t, err)
	require.Empty(t, req.Header.Get("Content-Encoding"))
}

func TestGrokRequestCompressionIsIsolatedByOwnerProxyAndTarget(t *testing.T) {
	resetGrokRequestCompressionCapabilityCacheForTest()
	t.Cleanup(resetGrokRequestCompressionCapabilityCacheForTest)

	upstream := &grokCompressionUpstreamStub{settingsBody: `{"accept_request_encodings":["zstd"]}`}
	svc := &OpenAIGatewayService{cfg: grokCompressionEnabledConfig(), httpUpstream: upstream}
	body := grokCompressionPaddedJSON(t, grokRequestCompressionMinBytes*2)

	proxied := grokCompressionTestAccount(21)
	proxied.ProxyID = grokCompressionPtrInt64(7)
	proxied.Proxy = &Proxy{ID: 7, Protocol: "http", Host: "proxy.example.test", Port: 8080}

	// Different owner: the cache entry does not transfer.
	reqA, err := svc.buildGrokResponsesRequest(context.Background(), nil, grokCompressionTestAccount(21), body, "access-token", "", svc.cfg)
	require.NoError(t, err)
	require.Equal(t, "zstd", reqA.Header.Get("Content-Encoding"))

	// Different proxy for the same target/owner: separate capability identity,
	// and the probe uses that account's egress configuration.
	reqB, err := svc.buildGrokResponsesRequest(context.Background(), nil, proxied, body, "access-token", "", svc.cfg)
	require.NoError(t, err)
	require.Equal(t, "zstd", reqB.Header.Get("Content-Encoding"))

	settingsCalls, _ := upstream.snapshot()
	require.Equal(t, 2, settingsCalls, "owner and proxy each own their capability state")
	upstream.mu.Lock()
	proxies := append([]string(nil), upstream.proxies...)
	upstream.mu.Unlock()
	require.Equal(t, "", proxies[0])
	require.Contains(t, proxies[1], "proxy.example.test")

	// A destination that is not the trusted CLI proxy never negotiates.
	apiKeyAccount := grokCompressionTestAccount(22)
	apiKeyAccount.Type = AccountTypeAPIKey
	apiKeyAccount.Credentials["base_url"] = xai.DefaultBaseURL
	reqC, err := svc.buildGrokResponsesRequest(context.Background(), nil, apiKeyAccount, body, "api-key", "", svc.cfg)
	require.NoError(t, err)
	require.Empty(t, reqC.Header.Get("Content-Encoding"))
	settingsCallsAfter, _ := upstream.snapshot()
	require.Equal(t, 2, settingsCallsAfter)

	// An owner change re-evaluates capability and re-encodes from the final JSON.
	reqD, err := svc.buildGrokResponsesRequest(context.Background(), nil, grokCompressionTestAccount(23), body, "access-token", "", svc.cfg)
	require.NoError(t, err)
	require.Equal(t, "zstd", reqD.Header.Get("Content-Encoding"))
	require.Equal(t, body, decodeGrokCompressionBody(t, reqD))
}

func TestGrokRequestCompressionEncodeFailureFallsBackToPlainJSON(t *testing.T) {
	resetGrokRequestCompressionCapabilityCacheForTest()
	t.Cleanup(resetGrokRequestCompressionCapabilityCacheForTest)

	original := grokJSONBodyEncoder
	grokJSONBodyEncoder = func([]byte) ([]byte, error) { return nil, errors.New("encode failed") }
	t.Cleanup(func() { grokJSONBodyEncoder = original })

	upstream := &grokCompressionUpstreamStub{settingsBody: `{"accept_request_encodings":["zstd"]}`}
	svc := &OpenAIGatewayService{cfg: grokCompressionEnabledConfig(), httpUpstream: upstream}
	body := grokCompressionPaddedJSON(t, grokRequestCompressionMinBytes*3)

	req, err := svc.buildGrokResponsesRequest(context.Background(), nil, grokCompressionTestAccount(31), body, "access-token", "", svc.cfg)
	require.NoError(t, err)
	require.Empty(t, req.Header.Get("Content-Encoding"))
	require.Equal(t, int64(len(body)), req.ContentLength)
	require.Equal(t, body, decodeGrokPlainBody(t, req))
}

func TestGrokRequestCompressionLargeBodyUsesBoundedLimiter(t *testing.T) {
	resetGrokRequestCompressionCapabilityCacheForTest()
	t.Cleanup(resetGrokRequestCompressionCapabilityCacheForTest)

	upstream := &grokCompressionUpstreamStub{settingsBody: `{"accept_request_encodings":["zstd"]}`}
	svc := &OpenAIGatewayService{cfg: grokCompressionEnabledConfig(), httpUpstream: upstream}
	body := grokCompressionPaddedJSON(t, grokRequestCompressionOffloadBytes+1024)

	req, err := svc.buildGrokResponsesRequest(context.Background(), nil, grokCompressionTestAccount(41), body, "access-token", "", svc.cfg)
	require.NoError(t, err)
	require.Equal(t, "zstd", req.Header.Get("Content-Encoding"))
	require.Equal(t, body, decodeGrokCompressionBody(t, req))
}

// A compressed inference must keep the existing single-request accounting: one
// capability probe, one upstream inference send and one usage observation.
func TestGrokRequestCompressionKeepsSingleRequestAccounting(t *testing.T) {
	resetGrokRequestCompressionCapabilityCacheForTest()
	t.Cleanup(resetGrokRequestCompressionCapabilityCacheForTest)
	gin.SetMode(gin.TestMode)

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	body := []byte(`{"model":"grok","messages":[{"role":"user","content":"` + strings.Repeat("y", grokRequestCompressionMinBytes) + `"}],"stream":false,"stop":"done","prompt_cache_key":"raw-client-cache-key"}`)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(body))
	c.Set("api_key", &APIKey{ID: 5201})

	account := healthyGrokOAuthGatewayTestAccount(52, "access-token")
	account.Credentials["sub"] = "sub-owner"
	repo := &grokQuotaAccountRepo{
		mockAccountRepoForPlatform: &mockAccountRepoForPlatform{
			accountsByID: map[int64]*Account{52: account},
		},
	}
	upstream := &grokCompressionUpstreamStub{
		settingsBody:    `{"accept_request_encodings":["zstd"]}`,
		inferenceBody:   `{"id":"chatcmpl","object":"chat.completion","model":"grok-4.6","choices":[],"usage":{"prompt_tokens":1,"completion_tokens":2,"prompt_tokens_details":{"cached_tokens":1}}}`,
		inferenceStatus: http.StatusOK,
		inferenceHeader: http.Header{
			"Content-Type":                   []string{"application/json"},
			"Xai-Request-Id":                 []string{"xai-req"},
			"X-Ratelimit-Limit-Requests":     []string{"10"},
			"X-Ratelimit-Remaining-Requests": []string{"9"},
			"X-Ratelimit-Limit-Tokens":       []string{"1000"},
			"X-Ratelimit-Remaining-Tokens":   []string{"990"},
		},
	}
	svc := &OpenAIGatewayService{
		cfg:               grokCompressionEnabledConfig(),
		httpUpstream:      upstream,
		grokTokenProvider: NewGrokTokenProvider(repo, nil),
		accountRepo:       repo,
	}

	result, err := svc.ForwardAsChatCompletions(context.Background(), c, account, body, "", "")
	require.NoError(t, err)

	settingsCalls, inferenceCalls := upstream.snapshot()
	require.Equal(t, 1, settingsCalls, "one capability probe per target/owner window")
	require.Equal(t, 1, inferenceCalls, "a compressed request still sends exactly one inference")
	require.Equal(t, "zstd", upstream.requests[len(upstream.requests)-1].Header.Get("Content-Encoding"))
	decoded := decodeGrokCompressionBody(t, upstream.requests[len(upstream.requests)-1])
	require.Equal(t, "grok-4.6", gjson.GetBytes(decoded, "model").String())
	require.False(t, gjson.GetBytes(decoded, "prompt_cache_key").Exists())
	require.Equal(t, 1, result.Usage.InputTokens)
	require.Equal(t, 2, result.Usage.OutputTokens)
	require.Equal(t, 1, result.Usage.CacheReadInputTokens)
	require.NotNil(t, repo.updates[52][grokQuotaSnapshotExtraKey])
	require.Equal(t, http.StatusOK, recorder.Code)
}

// The encoded bytes must be coherent on the wire over plain HTTP/1.1 and over
// TLS: Content-Length matches the compressed body and the receiver can decode
// the final JSON with no stale declaration.
func TestGrokRequestCompressionWireSendOverHTTPAndTLS(t *testing.T) {
	resetGrokRequestCompressionCapabilityCacheForTest()
	t.Cleanup(resetGrokRequestCompressionCapabilityCacheForTest)

	type observed struct {
		encoding      string
		contentLength int64
		body          []byte
	}
	handler := func(seen *observed) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			body, _ := io.ReadAll(r.Body)
			seen.encoding = r.Header.Get("Content-Encoding")
			seen.contentLength = r.ContentLength
			seen.body = body
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"id":"ok"}`))
		}
	}

	for _, tc := range []struct {
		name   string
		server func(http.Handler) *httptest.Server
	}{
		{name: "http1", server: func(h http.Handler) *httptest.Server { return httptest.NewServer(h) }},
		{name: "tls", server: func(h http.Handler) *httptest.Server { return httptest.NewTLSServer(h) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var seen observed
			server := tc.server(handler(&seen))
			defer server.Close()

			upstream := &grokCompressionUpstreamStub{settingsBody: `{"accept_request_encodings":["zstd"]}`}
			svc := &OpenAIGatewayService{cfg: grokCompressionEnabledConfig(), httpUpstream: upstream}
			body := grokCompressionPaddedJSON(t, grokRequestCompressionMinBytes*2)

			// Build against the real CLI proxy target so negotiation runs, then
			// deliver the exact encoded request to a real H1/TLS server.
			req, err := svc.buildGrokResponsesRequest(context.Background(), nil, grokCompressionTestAccount(51), body, "access-token", "", svc.cfg)
			require.NoError(t, err)
			require.Equal(t, "zstd", req.Header.Get("Content-Encoding"))

			target, err := url.Parse(server.URL + "/v1/responses")
			require.NoError(t, err)
			req.URL = target
			req.Host = ""
			req.RequestURI = ""

			client := server.Client()
			resp, err := client.Do(req)
			require.NoError(t, err)
			defer func() { _ = resp.Body.Close() }()
			_, _ = io.ReadAll(resp.Body)

			require.Equal(t, "zstd", seen.encoding)
			require.Equal(t, int64(len(seen.body)), seen.contentLength)
			decoded, err := zstd.NewReader(bytes.NewReader(seen.body))
			require.NoError(t, err)
			defer decoded.Close()
			plain, err := io.ReadAll(decoded)
			require.NoError(t, err)
			require.Equal(t, body, plain)
		})
	}
}
