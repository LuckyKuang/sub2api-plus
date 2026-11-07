//go:build unit

package service

import (
	"bytes"
	"context"
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

// Official kimi-code eb2035f3, builtin/provider/kimi/protocol.ts: explicit
// enabled/disabled thinking selects context-management instead of the generic
// Anthropic interleaved beta. Both credential types have the same wire contract.
func TestKimiOfficialThinkingBetaOnFinalSend(t *testing.T) {
	for _, kind := range []string{AccountTypeOAuth, AccountTypeAPIKey} {
		for _, thinking := range []string{"enabled", "disabled"} {
			t.Run(kind+"/"+thinking, func(t *testing.T) {
				body := []byte(fmt.Sprintf(`{"model":"kimi-k3","thinking":{"type":%q},"messages":[{"role":"user","content":"hello"}]}`, thinking))
				req, err := http.NewRequest(http.MethodPost, "https://api.kimi.com/coding/v1/messages", bytes.NewReader(body))
				require.NoError(t, err)
				req.Header.Set("anthropic-beta", "claude-code-20250219,interleaved-thinking-2025-05-14")
				upstream := &httpUpstreamRecorder{resp: nativeAnthropicBufferedResponse()}
				svc := &OpenAIGatewayService{httpUpstream: upstream}
				account := &Account{ID: 32, Platform: PlatformKimi, Type: kind}
				_, err = svc.doOpenAIUpstream(req, "", account)
				require.NoError(t, err)
				require.Equal(t, "context-management-2025-06-27", upstream.lastReq.Header.Get("Anthropic-Beta"))
				require.Equal(t, "kimi-code-cli/2.5.2", upstream.lastReq.UserAgent())
				require.JSONEq(t, string(body), string(upstream.lastBody))
			})
		}
	}
}

// ZCode a9fe401e runner-attribution.ts permits session-features only from its
// own runtime context. This gateway has no workflow execution facts to report.
func TestZCodeSessionFeaturesCannotComeFromCallerOrStoredOverride(t *testing.T) {
	for _, platform := range []string{PlatformZhipu, PlatformKimi, PlatformDeepseek, PlatformMiniMax, PlatformStepFun} {
		for _, kind := range []string{AccountTypeOAuth, AccountTypeAPIKey} {
			for _, path := range []string{"/v1/messages", "/v1/chat/completions", "/v1/responses", "/models"} {
				t.Run(platform+"/"+kind+path, func(t *testing.T) {
					req, err := http.NewRequest(http.MethodPost, "https://provider.example"+path, strings.NewReader(`{"model":"native","messages":[]}`))
					require.NoError(t, err)
					req.Header["X-Zcode-Session-Features"] = []string{"workflow_cmd"}
					req.Header["x-zcode-session-features"] = []string{"workflow_auto"}
					account := &Account{ID: 42, Platform: platform, Type: kind, Credentials: map[string]any{
						credKeyHeaderOverrideEnabled: true, credKeyHeaderOverrides: map[string]any{"X-ZCODE-SESSION-FEATURES": "workflow_auto"},
					}}
					prepareAccountOutboundRequest(req, account)
					require.NoError(t, OutboundPreparationError(req.Context()))
					for name := range req.Header {
						require.False(t, strings.EqualFold(name, "x-zcode-session-features"), name)
					}
				})
			}
		}
	}
}

// Codex 41cbd5b9b2 common.rs/turn_input.rs: the optional TTL is 30m or 12h,
// applies to one turn, and is serialized on both Responses HTTP and WebSocket.
func TestCodexOfficialPromptCacheTTLPreservedAcrossTransportsAndTurns(t *testing.T) {
	for _, kind := range []string{AccountTypeOAuth, AccountTypeSetupToken, AccountTypeAPIKey} {
		for _, transport := range []string{"http", "passthrough", "websocket", "websocket_http_bridge"} {
			t.Run(kind+"/"+transport, func(t *testing.T) {
				account := &Account{ID: 91, Platform: PlatformOpenAI, Type: kind, Concurrency: 1, Credentials: map[string]any{"access_token": "fixture", "api_key": "fixture"}}
				for _, ttl := range []string{"30m", "12h", ""} {
					options := ""
					if ttl != "" {
						options = fmt.Sprintf(`,"prompt_cache_options":{"ttl":%q}`, ttl)
					}
					body := []byte(`{"model":"gpt-6.1-sol","input":"hello","stream":true` + options + `}`)
					if transport == "websocket" || transport == "websocket_http_bridge" {
						body = []byte(`{"type":"response.create",` + string(body[1:]))
					}
					c, _ := gin.CreateTestContext(httptest.NewRecorder())
					c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(body))
					response := `data: {"type":"response.completed","response":{"id":"resp_ttl","model":"gpt-6.1-sol","output":[],"usage":{"input_tokens":1,"output_tokens":1}}}` + "\n\n"
					upstream := &httpUpstreamRecorder{resp: &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(response))}}
					svc := &OpenAIGatewayService{cfg: &config.Config{Gateway: config.GatewayConfig{MaxLineSize: defaultMaxLineSize}}, httpUpstream: upstream}
					var sent []byte
					var err error
					switch transport {
					case "http", "passthrough":
						if transport == "passthrough" {
							account.Extra = map[string]any{"openai_passthrough": true}
						}
						_, err = svc.Forward(context.Background(), c, account, body)
						sent = upstream.lastBody
					case "websocket":
						sent, _, err = normalizeOpenAIResponsesWebSocketCompatibilityBody(body, account, false)
						if err == nil {
							sent, _, err = normalizeOpenAIPromptCacheControlsForAccount(sent, account, "gpt-6.1-sol")
						}
					case "websocket_http_bridge":
						_, err = svc.proxyOpenAIWSHTTPBridgeTurn(context.Background(), c, account, "fixture", body, len(body), "gpt-6.1-sol", "", "", "", "", 1, func([]byte) error { return nil })
						sent = upstream.lastBody
					}
					require.NoError(t, err)
					require.Equal(t, ttl != "", gjson.GetBytes(sent, "prompt_cache_options").Exists(), string(sent))
					require.Equal(t, ttl, gjson.GetBytes(sent, "prompt_cache_options.ttl").String())
				}
			})
		}
	}
}
