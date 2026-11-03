//go:build unit

package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/LuckyKuang/sub2api-plus/internal/config"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

// User contract: configuration is opt-in, binds every model to exactly one
// inference interface, and must behave identically on cold and cached auth.
func TestCompositeUnifiedEndpointAdmissionAndAuthCache(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, configured := range []string{"", "/v1/responses", "/v1/messages", "/v1/chat/completions"} {
		for _, cached := range []bool{false, true} {
			key := &APIKey{ConversionEnabled: configured != "", DefaultClient: "custom", DefaultEndpoint: configured, User: &User{ID: 7}, Group: &Group{Platform: PlatformComposite}}
			if cached {
				svc := &APIKeyService{}
				payload, err := json.Marshal(svc.snapshotFromAPIKey(context.Background(), key))
				require.NoError(t, err)
				var snapshot APIKeyAuthSnapshot
				require.NoError(t, json.Unmarshal(payload, &snapshot))
				key = svc.snapshotToAPIKey("fixture", &snapshot)
				require.Equal(t, configured, key.DefaultEndpoint)
				require.Equal(t, "custom", key.DefaultClient)
			}
			for _, path := range []string{"/v1/responses", "/v1/messages", "/v1/chat/completions", "/v1beta/models/gemini:generateContent", "/v1/responses/compact", "/v1/systemone"} {
				t.Run(fmt.Sprintf("%s/cache=%t/%s", configured, cached, path), func(t *testing.T) {
					router := gin.New()
					calls := 0
					router.Use(func(c *gin.Context) {
						if BindCompositeAccessPolicy(c, key) {
							c.Next()
						}
					})
					router.POST(path, func(c *gin.Context) { calls++; c.Status(204) })
					w := httptest.NewRecorder()
					router.ServeHTTP(w, httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{"model":"any-model"}`)))
					if configured == "" || configured == path {
						require.Equal(t, 204, w.Code)
						require.Equal(t, 1, calls)
					} else {
						require.Equal(t, 400, w.Code)
						require.Zero(t, calls, "reject before audit, scheduling, billing or upstream writes")
						require.Contains(t, w.Body.String(), "composite_endpoint_required")
					}
				})
			}
		}
	}
	// A previously cached snapshot lacks the enforced policy and must reload.
	_, used, err := (&APIKeyService{}).applyAuthCacheEntry("fixture", &APIKeyAuthCacheEntry{Snapshot: &APIKeyAuthSnapshot{Version: 26}})
	require.NoError(t, err)
	require.False(t, used)
}

func TestCompositeUnifiedClearingRestoresNativeAdmission(t *testing.T) {
	key := &APIKey{ConversionEnabled: true, DefaultEndpoint: "/v1/responses", Group: &Group{Platform: PlatformComposite}}
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("POST", "/v1/messages", nil)
	require.False(t, BindCompositeAccessPolicy(c, key))
	key.ConversionEnabled = false
	key.DefaultEndpoint, key.DefaultClient = "", ""
	c, _ = gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/v1/messages", nil)
	require.True(t, BindCompositeAccessPolicy(c, key))
	require.Equal(t, APIProtocolAnthropic, CompositeNativeProtocol(c.Request.Context()))
	for _, path := range []string{"/v1/models", "/v1/models/gpt-model"} {
		key.ConversionEnabled = true
		key.DefaultEndpoint = "/v1/responses"
		c.Request = httptest.NewRequest("GET", path, nil)
		require.True(t, BindCompositeAccessPolicy(c, key))
	}
	c.Request = httptest.NewRequest("GET", "/v1/responses", nil)
	require.False(t, BindCompositeAccessPolicy(c, key), "HTTP conversion must not advertise a WebSocket bridge")
}

func TestCompositeUnifiedResponsesConvertsAnthropicWire(t *testing.T) {
	for _, stream := range []bool{false, true} {
		body := []byte(fmt.Sprintf(`{"model":"claude-sonnet-4-5","input":"hello","stream":%t}`, stream))
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest("POST", "/v1/responses", bytes.NewReader(body))
		key := &APIKey{ConversionEnabled: true, DefaultEndpoint: "/v1/responses", Group: &Group{Platform: PlatformComposite}}
		c.Set("api_key", key)
		require.True(t, BindCompositeAccessPolicy(c, key))
		upstream := &anthropicHTTPUpstreamRecorder{resp: &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(namespaceToolAnthropicStream()))}}
		svc := &GatewayService{cfg: &config.Config{}, httpUpstream: upstream}
		account := &Account{ID: 1, Platform: PlatformAnthropic, Type: AccountTypeAPIKey, Credentials: map[string]any{"api_key": "fixture"}}
		result, err := svc.ForwardAsResponses(c.Request.Context(), c, account, body, nil)
		require.NoError(t, err)
		require.NotNil(t, result)
		require.True(t, strings.HasSuffix(upstream.lastReq.URL.Path, "/messages"))
		require.Equal(t, "user", gjson.GetBytes(upstream.lastBody, "messages.0.role").String())
		content := gjson.GetBytes(upstream.lastBody, "messages.0.content")
		if content.Type == gjson.String {
			require.Equal(t, "hello", content.String())
		} else {
			require.Equal(t, "hello", content.Get("0.text").String())
		}
		require.NotEmpty(t, upstream.lastReq.Header.Get("User-Agent"))
	}
}

func TestCompositeUnifiedResponsesConvertsGeminiWireAndUsage(t *testing.T) {
	for _, stream := range []bool{false, true} {
		body := []byte(fmt.Sprintf(`{"model":"gemini-2.5-flash","input":"hello","stream":%t}`, stream))
		responseBody := `{"candidates":[{"content":{"parts":[{"text":"hello from gemini"}]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":12,"cachedContentTokenCount":5,"candidatesTokenCount":3}}`
		if stream {
			responseBody = "data: " + responseBody + "\n\n"
		}
		upstream := &geminiCompatHTTPUpstreamStub{response: &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(responseBody))}}
		svc := &GeminiMessagesCompatService{cfg: &config.Config{}, httpUpstream: upstream}
		account := &Account{ID: 2, Platform: PlatformGemini, Type: AccountTypeAPIKey, Credentials: map[string]any{"api_key": "fixture"}}
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest("POST", "/v1/responses", bytes.NewReader(body))
		key := &APIKey{ConversionEnabled: true, DefaultEndpoint: "/v1/responses", Group: &Group{Platform: PlatformComposite}}
		c.Set("api_key", key)
		require.True(t, BindCompositeAccessPolicy(c, key))
		result, err := svc.ForwardAsResponses(c.Request.Context(), c, account, body)
		require.NoError(t, err)
		require.NotNil(t, result)
		require.Contains(t, upstream.lastReq.URL.Path, ":"+map[bool]string{false: "generateContent", true: "streamGenerateContent"}[stream])
		sent, err := io.ReadAll(upstream.lastReq.Body)
		require.NoError(t, err)
		require.Contains(t, string(sent), "hello")
		require.NotContains(t, string(sent), `"input"`)
		require.NotEmpty(t, upstream.lastReq.Header.Get("User-Agent"))
		require.Equal(t, 5, result.Usage.CacheReadInputTokens)
		require.Contains(t, w.Body.String(), "hello from gemini")
		if stream {
			require.Contains(t, w.Body.String(), "event: response.completed")
			require.NotContains(t, w.Body.String(), "data: [DONE]")
		} else {
			require.Equal(t, "response", gjson.GetBytes(w.Body.Bytes(), "object").String())
		}
	}
}

func TestCompositeUnifiedRejectsLosingProviderContinuationBeforeWire(t *testing.T) {
	body := []byte(`{"model":"gemini-2.5-flash","input":"next","previous_response_id":"resp_provider_owned"}`)
	upstream := &geminiCompatHTTPUpstreamStub{}
	svc := &GeminiMessagesCompatService{cfg: &config.Config{}, httpUpstream: upstream}
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("POST", "/v1/responses", bytes.NewReader(body))
	key := &APIKey{ConversionEnabled: true, DefaultEndpoint: "/v1/responses", Group: &Group{Platform: PlatformComposite}}
	c.Set("api_key", key)
	require.True(t, BindCompositeAccessPolicy(c, key))
	result, err := svc.ForwardAsResponses(c.Request.Context(), c, &Account{Platform: PlatformGemini, Type: AccountTypeAPIKey}, body)
	require.Error(t, err)
	require.Nil(t, result)
	require.Zero(t, upstream.calls)
	require.Equal(t, 400, w.Code)
	require.Contains(t, w.Body.String(), "composite_native_continuation_required")
}

// Native mode forbids a Gemini Chat bridge at every sending entrypoint. Explicit
// unified Chat consent must still allow that same real Gemini wire operation.
func TestCompositeGeminiChatRequiresConversionConsentBeforeWire(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		t.Run(fmt.Sprintf("conversion=%t", enabled), func(t *testing.T) {
			body := []byte(`{"model":"gemini-2.5-flash","messages":[{"role":"user","content":"hello"}]}`)
			upstream := &geminiCompatHTTPUpstreamStub{response: &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{"candidates":[{"content":{"parts":[{"text":"hello from gemini"}]},"finishReason":"STOP"}]}`))}}
			svc := &GeminiMessagesCompatService{cfg: &config.Config{}, httpUpstream: upstream}
			account := &Account{ID: 2, Platform: PlatformGemini, Type: AccountTypeAPIKey, Credentials: map[string]any{"api_key": "fixture"}}
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(body))
			key := &APIKey{ConversionEnabled: enabled, DefaultEndpoint: "/v1/chat/completions", Group: &Group{Platform: PlatformComposite}}
			c.Set("api_key", key)
			require.True(t, BindCompositeAccessPolicy(c, key))
			result, err := svc.ForwardAsChatCompletions(c.Request.Context(), c, account, body)
			if !enabled {
				require.Error(t, err)
				require.Nil(t, result)
				require.Zero(t, upstream.calls)
				require.Equal(t, http.StatusBadRequest, w.Code)
				require.Contains(t, w.Body.String(), "native_endpoint_unsupported")
				return
			}
			require.NoError(t, err)
			require.NotNil(t, result)
			require.Equal(t, 1, upstream.calls)
			require.Contains(t, upstream.lastReq.URL.Path, ":generateContent")
			require.Contains(t, w.Body.String(), "hello from gemini")
		})
	}
}

func TestCompositeUnifiedTerminalRequiresAuthenticatedPolicyMarker(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/v1/responses", nil)
	key := &APIKey{ConversionEnabled: true, DefaultEndpoint: "/v1/responses", Group: &Group{Platform: PlatformComposite}}
	c.Set("api_key", key)
	account := &Account{Platform: PlatformOpenAI, Type: AccountTypeAPIKey}
	require.Error(t, CheckCompositeNativeForward(WithCompositeNativeProtocol(context.Background(), APIProtocolResponses), c, account, APIProtocolResponses, "gpt-5.4"))
}

func TestCompositeLegacyPreferencesDoNotEnableConversion(t *testing.T) {
	for _, cached := range []bool{false, true} {
		key := &APIKey{User: &User{ID: 7}, DefaultClient: "codex", DefaultEndpoint: "/v1/responses", Group: &Group{Platform: PlatformComposite}}
		if cached {
			svc := &APIKeyService{}
			key = svc.snapshotToAPIKey("legacy", svc.snapshotFromAPIKey(context.Background(), key))
		}
		require.False(t, key.ConversionEnabled)
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest("POST", "/v1/messages", nil)
		c.Set("api_key", key)
		require.True(t, BindCompositeAccessPolicy(c, key))
		require.Empty(t, CompositeUnifiedEndpoint(c.Request.Context()))
		require.NoError(t, CheckCompositeNativeForward(c.Request.Context(), c, &Account{Platform: PlatformAnthropic}, APIProtocolAnthropic, "claude"))
	}
}

func TestCompositeGeminiResponsesPreservesTrustedIdentitySource(t *testing.T) {
	for _, source := range []string{"compiled", "global", "account", "invalid_account"} {
		t.Run(source, func(t *testing.T) {
			settings := emptyOutboundIdentitySettings()
			wantUA := "GeminiCLI/0.1.5 (Linux; x64)"
			if source != "compiled" {
				settings.Profiles["gemini"] = OutboundIdentitySelection{Preset: "gemini", Version: "3.9.1"}
				wantUA = "GeminiCLI/3.9.1 (Linux; x64)"
			}
			_, ctx := outboundIdentityTestSettings(t, settings)
			account := &Account{ID: 42, Platform: PlatformGemini, Type: AccountTypeAPIKey, Credentials: map[string]any{"api_key": "fixture"}}
			if source == "account" {
				account.Credentials[outboundIdentityCredential] = OutboundIdentitySelection{Preset: "gemini", Version: "3.9.2"}
				wantUA = "GeminiCLI/3.9.2 (Linux; x64)"
			} else if source == "invalid_account" {
				account.Credentials[outboundIdentityCredential] = OutboundIdentitySelection{Preset: "gemini", UserAgent: "inbound/999.0.0"}
			}
			captured := make(chan http.Header, 1)
			upstream := geminiControlWireUpstream(t, func(w http.ResponseWriter, req *http.Request) {
				captured <- req.Header.Clone()
				w.Header().Set("Content-Type", "application/json")
				_, _ = io.WriteString(w, `{"candidates":[{"content":{"parts":[{"text":"ok"}]},"finishReason":"STOP"}]}`)
			})
			body := []byte(`{"model":"gemini-2.5-flash","input":"hello"}`)
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest("POST", "/v1/responses", bytes.NewReader(body)).WithContext(ctx)
			c.Request.Header.Set("User-Agent", "untrusted-inbound/999")
			c.Request.Header.Set("Originator", "untrusted-inbound")
			c.Request.Header.Set("Version", "999")
			key := &APIKey{ConversionEnabled: true, DefaultEndpoint: "/v1/responses", Group: &Group{Platform: PlatformComposite}}
			c.Set("api_key", key)
			require.True(t, BindCompositeAccessPolicy(c, key))
			svc := &GeminiMessagesCompatService{cfg: &config.Config{}, httpUpstream: upstream}
			_, err := svc.ForwardAsResponses(c.Request.Context(), c, account, body)
			require.NoError(t, err)
			headers := <-captured
			require.Equal(t, wantUA, headers.Get("User-Agent"))
			for _, name := range []string{"Originator", "Version", "X-Goog-Api-Client", "X-Stainless-Package-Version"} {
				require.Empty(t, headers.Get(name), "Gemini preset must not emit foreign identity headers")
			}
		})
	}
}

// Requirement: the Anthropic Messages interface exposes count_tokens as a
// non-inference auxiliary of the same endpoint, so a unified Messages key must
// admit it. The other unified endpoints have no auxiliary call, so any other
// sub-path stays rejected.
func TestCompositeUnifiedMessagesAdmitsCountTokensOnly(t *testing.T) {
	gin.SetMode(gin.TestMode)
	admit := func(endpoint, path string) (int, int) {
		key := &APIKey{ConversionEnabled: true, DefaultClient: "custom", DefaultEndpoint: endpoint, Group: &Group{Platform: PlatformComposite}}
		router := gin.New()
		calls := 0
		router.Use(func(c *gin.Context) {
			if BindCompositeAccessPolicy(c, key) {
				c.Next()
			}
		})
		router.POST(path, func(c *gin.Context) { calls++; c.Status(204) })
		w := httptest.NewRecorder()
		router.ServeHTTP(w, httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{"model":"any-model"}`)))
		return w.Code, calls
	}

	code, calls := admit("/v1/messages", "/v1/messages/count_tokens")
	require.Equal(t, 204, code)
	require.Equal(t, 1, calls, "token counting is not inference and belongs to the Messages endpoint")

	code, calls = admit("/v1/messages", "/v1/messages/batches")
	require.Equal(t, 400, code)
	require.Zero(t, calls, "only count_tokens is an auxiliary of the Messages endpoint")

	for _, endpoint := range []string{"/v1/responses", "/v1/chat/completions"} {
		code, calls = admit(endpoint, "/v1/messages/count_tokens")
		require.Equal(t, 400, code)
		require.Zero(t, calls, "count_tokens is not an auxiliary of the "+endpoint+" endpoint")
	}
}

// Requirement: every composite sender is a terminal defense point. A failover or
// retry that lands on a credential which cannot carry the ingress protocol must
// fail before any upstream write, instead of silently bridging it. Each sender
// below previously lacked this guard.
func TestCompositeSendersRejectIncompatibleCredentialBeforeUpstream(t *testing.T) {
	gin.SetMode(gin.TestMode)
	body := []byte(`{"model":"claude-sonnet-4-5","messages":[{"role":"user","content":"hi"}]}`)

	nativeContext := func(protocol string) (*gin.Context, *httptest.ResponseRecorder) {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest("POST", "/v1/messages", bytes.NewReader(body))
		key := &APIKey{Group: &Group{Platform: PlatformComposite}}
		c.Set("api_key", key)
		c.Request = c.Request.WithContext(WithCompositeNativeProtocol(c.Request.Context(), protocol))
		return c, w
	}

	// An Anthropic credential cannot carry Responses or Chat Completions.
	anthropicOnly := &Account{ID: 9, Platform: PlatformAnthropic, Type: AccountTypeAPIKey}

	t.Run("gemini messages forward", func(t *testing.T) {
		c, w := nativeContext(APIProtocolAnthropic)
		svc := &GeminiMessagesCompatService{}
		result, err := svc.Forward(c.Request.Context(), c, &Account{ID: 3, Platform: PlatformOpenAI, Type: AccountTypeAPIKey}, body)
		require.Error(t, err)
		require.Nil(t, result)
		require.Contains(t, w.Body.String(), "native_endpoint_unsupported")
	})

	t.Run("antigravity responses forward", func(t *testing.T) {
		c, w := nativeContext(APIProtocolResponses)
		svc := &AntigravityGatewayService{}
		result, err := svc.ForwardAsResponses(c.Request.Context(), c, anthropicOnly, body, nil)
		require.Error(t, err)
		require.Nil(t, result)
		require.Contains(t, w.Body.String(), "native_endpoint_unsupported")
	})

	t.Run("antigravity chat completions forward", func(t *testing.T) {
		c, w := nativeContext(APIProtocolChatCompletions)
		svc := &AntigravityGatewayService{}
		result, err := svc.ForwardAsChatCompletions(c.Request.Context(), c, anthropicOnly, body, nil)
		require.Error(t, err)
		require.Nil(t, result)
		require.Contains(t, w.Body.String(), "native_endpoint_unsupported")
	})

	// The guard must stay a guard: a credential that can carry the protocol is
	// still admitted past this point.
	t.Run("admits a credential that carries the protocol", func(t *testing.T) {
		c, _ := nativeContext(APIProtocolAnthropic)
		require.NoError(t, CheckCompositeNativeForward(c.Request.Context(), c, anthropicOnly, APIProtocolAnthropic, "claude-sonnet-4-5"))
	})
}
