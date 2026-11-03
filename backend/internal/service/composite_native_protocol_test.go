//go:build unit

package service

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

// A supplier's model catalog adds native endpoints without triggering an
// ingress-time refresh, while credential rotation and explicit rules isolate it.
func TestCompositeNativeCredentialScopedCatalog(t *testing.T) {
	account := commandCodeTestAccount(301)
	account.Credentials["api_base_urls"] = map[string]any{APIProtocolChatCompletions: "https://composite-catalog.example/v1"}
	ctx := WithAccountOutboundIdentity(context.Background(), account)
	url := buildOpenAIModelsURL(account.GetCNProtocolBaseURL(APIProtocolChatCompletions))
	key := modelProtocolCatalogKey(account, url, modelProtocolCatalogHeaders(ctx, account), "")
	upstreamModelProtocols.store(key, map[string][]string{"claude-native": {APIProtocolAnthropic, APIProtocolResponses}}, nil, time.Now())
	t.Cleanup(func() {
		upstreamModelProtocols.mu.Lock()
		delete(upstreamModelProtocols.entries, key)
		upstreamModelProtocols.mu.Unlock()
	})
	require.ElementsMatch(t, []string{APIProtocolAnthropic, APIProtocolResponses}, account.NativeProtocols("claude-native"))
	require.True(t, compositeNativeAccountAllowed(WithCompositeNativeProtocol(ctx, APIProtocolResponses), account, "claude-native"))
	account.Credentials["api_key"] = "rotated-fixture"
	require.Equal(t, []string{APIProtocolAnthropic}, account.NativeProtocols("claude-native"))
	account.Credentials["protocol_rules"] = []any{map[string]any{"pattern": "claude-*", "protocols": []any{APIProtocolChatCompletions}}}
	require.Equal(t, []string{APIProtocolChatCompletions}, account.NativeProtocols("claude-native"))
}

// User contract: no Composite request, including failover, may acquire a
// credential that would require a protocol bridge.
func TestCompositeNativeCandidates(t *testing.T) {
	tests := []struct {
		platform, kind, protocol string
		allowed                  bool
	}{
		{PlatformAnthropic, AccountTypeAPIKey, APIProtocolResponses, false},
		{PlatformAnthropic, AccountTypeOAuth, APIProtocolAnthropic, true},
		{PlatformOpenAI, AccountTypeOAuth, APIProtocolChatCompletions, false},
		{PlatformOpenAI, AccountTypeOAuth, APIProtocolResponses, true},
		{PlatformOpenAI, AccountTypeAPIKey, APIProtocolChatCompletions, true},
		{PlatformOpenAI, AccountTypeUpstream, APIProtocolResponses, false},
		{PlatformOpenAI, AccountTypeUpstream, APIProtocolChatCompletions, false},
		{PlatformGemini, AccountTypeAPIKey, APIProtocolAnthropic, false},
		{PlatformGemini, AccountTypeAPIKey, "gemini", true},
		{PlatformZhipu, AccountTypeAPIKey, APIProtocolResponses, false},
	}
	for _, tc := range tests {
		t.Run(tc.platform+tc.kind+tc.protocol, func(t *testing.T) {
			account := &Account{Platform: tc.platform, Type: tc.kind, Status: StatusActive, Credentials: map[string]any{"api_protocol": APIProtocolAdaptive}}
			require.Equal(t, tc.allowed, compositeNativeAccountAllowed(WithCompositeNativeProtocol(context.Background(), tc.protocol), account, "model"))
		})
	}
}

func TestCompositeNativeTerminalBridgeRejectedBeforeUpstream(t *testing.T) {
	gin.SetMode(gin.TestMode)
	body := []byte(`{"model":"claude-opus-5-5","input":"Hello"}`)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest("POST", "/v1/responses", bytes.NewReader(body))
	c.Set("api_key", &APIKey{Group: &Group{Platform: PlatformComposite}})
	upstream := &httpUpstreamRecorder{}
	gateway := &GatewayService{httpUpstream: upstream}
	got, err := gateway.ForwardAsResponses(WithCompositeNativeProtocol(context.Background(), APIProtocolResponses), c, &Account{Platform: PlatformAnthropic}, body, nil)
	require.Error(t, err)
	require.Nil(t, got)
	require.Equal(t, 400, rec.Code)
	require.Contains(t, rec.Body.String(), "native_endpoint_unsupported")
	require.Nil(t, upstream.lastReq)
}

// Native Chat must retain cache-bearing fields and conversation/tool history,
// use /chat/completions on the wire and retain trusted outbound identity setup.
func TestCompositeNativeChatPreservesCacheAndHistory(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, endpoint := range []string{"", "/v1/chat/completions"} {
		body := []byte(`{"model":"gpt-5.4","messages":[{"role":"system","content":"stable prefix"},{"role":"user","content":"hello"}],"prompt_cache_key":"stable-cache","prompt_cache_retention":"24h","stream":false}`)
		rec := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(rec)
		c.Request = httptest.NewRequest("POST", "/v1/chat/completions", bytes.NewReader(body))
		key := &APIKey{ConversionEnabled: endpoint != "", DefaultEndpoint: endpoint, Group: &Group{Platform: PlatformComposite}}
		c.Set("api_key", key)
		require.True(t, BindCompositeAccessPolicy(c, key))
		upstream := &httpUpstreamRecorder{resp: &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(`{"id":"c1","object":"chat.completion","model":"gpt-5.4","choices":[{"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":10,"completion_tokens":1}}`))}}
		svc := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: upstream}
		result, err := svc.ForwardAsChatCompletions(c.Request.Context(), c, rawChatCompletionsTestAccount(), body, "", "")
		require.NoError(t, err)
		require.NotNil(t, result)
		require.NotNil(t, upstream.lastReq)
		require.True(t, strings.HasSuffix(upstream.lastReq.URL.Path, "/chat/completions"))
		require.Equal(t, "stable-cache", gjson.GetBytes(upstream.lastBody, "prompt_cache_key").String())
		require.Equal(t, "24h", gjson.GetBytes(upstream.lastBody, "prompt_cache_retention").String())
		require.JSONEq(t, gjson.GetBytes(body, "messages").Raw, gjson.GetBytes(upstream.lastBody, "messages").Raw)
		require.NotEmpty(t, upstream.lastReq.Header.Get("User-Agent"))
	}
}

func TestNativeAccessValidationAndUnconfiguredProtocol(t *testing.T) {
	require.NoError(t, ValidateNativeAccessPreference("codex", "/v1/responses"))
	require.Error(t, ValidateNativeAccessPreference("codex", "/v1/messages"))
	require.Error(t, ValidateNativeAccessPreference("unknown", "/v1/responses"))
	svc := &OpenAIGatewayService{}
	ctx := WithCompositeNativeProtocol(context.Background(), APIProtocolAnthropic)
	require.Equal(t, APIProtocolAnthropic, svc.resolveUpstreamProtocolFor(ctx, &Account{Platform: PlatformOpenAI}, APIProtocolAnthropic, "model"))
}

// A missing ingress marker must fail closed instead of allowing an internal
// caller to re-enter ordinary compatibility routing and send another protocol.
func TestCompositeNativeUnmarkedSenderDoesNotWriteUpstream(t *testing.T) {
	gin.SetMode(gin.TestMode)
	body := []byte(`{"model":"gpt-5.4","messages":[{"role":"user","content":"hello"}],"stream":false}`)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest("POST", "/v1/chat/completions", bytes.NewReader(body))
	c.Set("api_key", &APIKey{Group: &Group{Platform: PlatformComposite}})
	upstream := &httpUpstreamRecorder{err: io.EOF}
	svc := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: upstream}
	result, err := svc.ForwardAsChatCompletions(context.Background(), c, rawChatCompletionsTestAccount(), body, "", "")
	require.Error(t, err)
	require.Nil(t, result)
	require.Equal(t, 400, rec.Code)
	require.Nil(t, upstream.lastReq)
}
