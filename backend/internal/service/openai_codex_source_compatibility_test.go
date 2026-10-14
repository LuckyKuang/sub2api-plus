//go:build unit

package service

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

// Audit oracle: official codex 1b6e0d8145 core/src/client.rs:1034-1039.
// ConcurrentReasoningSummaries is opt-in; when supplied its wire option must survive.
func TestCodexSourceAuditReasoningStreamOption(t *testing.T) {
	const input = `{"model":"gpt-6.1-sol","input":[{"role":"user","content":"hello"}],"instructions":"test","stream":true,"store":false,"reasoning":{"summary":"auto"},"stream_options":{"reasoning_summary_delivery":"sequential_cutoff"}}`
	t.Run("oauth_normal", func(t *testing.T) {
		var body map[string]any
		require.NoError(t, json.Unmarshal([]byte(input), &body))
		result := applyCodexOAuthTransform(body, true, false)
		require.NoError(t, result.Error)
		encoded, err := json.Marshal(body)
		require.NoError(t, err)
		require.Equal(t, "sequential_cutoff", gjson.GetBytes(encoded, "stream_options.reasoning_summary_delivery").String())
	})
	t.Run("oauth_passthrough", func(t *testing.T) {
		body, _, err := normalizeOpenAIPassthroughOAuthBody([]byte(input), false)
		require.NoError(t, err)
		require.Equal(t, "sequential_cutoff", gjson.GetBytes(body, "stream_options.reasoning_summary_delivery").String())
	})
	t.Run("oauth_websocket", func(t *testing.T) {
		account := &Account{ID: 777, Platform: PlatformOpenAI, Type: AccountTypeOAuth}
		body, _, err := normalizeOpenAIResponsesWebSocketCompatibilityBody([]byte(input), account, false)
		require.NoError(t, err)
		require.Equal(t, "sequential_cutoff", gjson.GetBytes(body, "stream_options.reasoning_summary_delivery").String())
	})
}

// Audit oracle: official core/src/client.rs:887-899. HTTP Lite uses a header,
// while WS response.create carries the corresponding client_metadata key.
func TestCodexSourceAuditHTTPWSLite(t *testing.T) {
	for _, lite := range []string{"true", "false", ""} {
		for _, kind := range []string{AccountTypeOAuth, AccountTypeAPIKey} {
			for _, passthrough := range []bool{false, true} {
				t.Run("lite="+lite+"/"+kind+"/"+map[bool]string{false: "normal", true: "passthrough"}[passthrough], func(t *testing.T) {
					captured := make(chan []byte, 1)
					upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						conn, err := (&websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}).Upgrade(w, r, nil)
						if err != nil {
							return
						}
						defer conn.Close()
						_, payload, err := conn.ReadMessage()
						if err != nil {
							return
						}
						captured <- payload
						_ = conn.WriteMessage(websocket.TextMessage, []byte(`{"type":"response.output_text.delta","delta":"OK","output_index":0,"content_index":0}`))
						_ = conn.WriteMessage(websocket.TextMessage, []byte(`{"type":"response.completed","response":{"id":"resp_audit","model":"gpt-6.1-sol","status":"completed","output":[],"usage":{"input_tokens":1,"output_tokens":1}}}`))
					}))
					defer upstream.Close()
					cfg := codexTransportTestConfig()
					settings := &SettingService{settingRepo: &openAIIdentitySettingRepoStub{values: map[string]string{}}}
					settings.storeCodexTransportSettings(CodexTransportSettings{WSEnabled: true, HTTPWSEnabled: true})
					svc := newOpenAIRejectedFieldTestService(nil)
					svc.cfg = cfg
					svc.openaiWSResolver = NewOpenAIWSProtocolResolver(cfg)
					svc.settingService = settings
					svc.getOpenAIWSConnPool().setClientDialerForTest(codexSourceRedirectDialer{target: "ws" + strings.TrimPrefix(upstream.URL, "http"), base: newDefaultOpenAIWSClientDialer()})
					defer svc.CloseOpenAIWSPool()
					account := newOpenAIRejectedFieldTestAccount()
					if kind == AccountTypeOAuth {
						account = newOpenAIOAuthNamespaceTestAccount()
						account.Extra = map[string]any{"openai_oauth_responses_websockets_v2_enabled": true}
					}
					account.Credentials["base_url"] = upstream.URL
					account.Extra["openai_apikey_responses_websockets_v2_enabled"] = true
					account.Extra["openai_passthrough"] = passthrough
					body := []byte(`{"model":"gpt-6.1-sol","instructions":"test","stream":true,"store":false,"input":[{"role":"user","content":"hello"}],"client_metadata":{"future_meta":"retained"},"stream_options":{"reasoning_summary_delivery":"sequential_cutoff"}}`)
					c := newOpenAIRejectedFieldTestContext(body)
					c.Request.Header.Set("x-openai-internal-codex-responses-lite", lite)
					SetOpenAIClientTransport(c, OpenAIClientTransportHTTP)
					_, err := svc.Forward(context.Background(), c, account, body)
					require.NoError(t, err)
					select {
					case payload := <-captured:
						if lite == "true" {
							require.Equal(t, "true", gjson.GetBytes(payload, "client_metadata.ws_request_header_x_openai_internal_codex_responses_lite").String())
						} else {
							require.False(t, gjson.GetBytes(payload, "client_metadata.ws_request_header_x_openai_internal_codex_responses_lite").Exists())
						}
						require.Equal(t, "retained", gjson.GetBytes(payload, "client_metadata.future_meta").String())
						require.Equal(t, "sequential_cutoff", gjson.GetBytes(payload, "stream_options.reasoning_summary_delivery").String())
					default:
						t.Fatal("no WS generation captured")
					}
				})
			}
		}
	}

}

type codexSourceRedirectDialer struct {
	target string
	base   openAIWSClientDialer
}

func (d codexSourceRedirectDialer) Dial(ctx context.Context, _ string, headers http.Header, proxy string) (openAIWSClientConn, int, http.Header, error) {
	return d.base.Dial(ctx, d.target, headers, proxy)
}

// Audit oracle: official codex-api/src/endpoint/responses.rs stream_encoded
// sets Accept: text/event-stream for both auth types.
func TestCodexSourceAuditAPIKeyAccept(t *testing.T) {
	for _, passthrough := range []bool{false, true} {
		t.Run(map[bool]string{false: "normal", true: "passthrough"}[passthrough], func(t *testing.T) {
			svc := &OpenAIGatewayService{cfg: codexTransportTestConfig()}
			account := &Account{ID: 777, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Credentials: map[string]any{"api_key": "fixture", "base_url": "https://api.openai.com/v1"}}
			body := []byte(`{"model":"gpt-6.1-sol","stream":true,"input":"hello"}`)
			c := newOpenAIRejectedFieldTestContext(body)
			c.Request.Header.Set("Accept", "text/event-stream")
			var req *http.Request
			var err error
			if passthrough {
				req, err = svc.buildUpstreamRequestOpenAIPassthrough(context.Background(), c, account, body, "fixture")
			} else {
				req, err = svc.buildUpstreamRequest(context.Background(), c, account, body, "fixture", true, "", true)
			}
			require.NoError(t, err)
			require.Equal(t, "text/event-stream", req.Header.Get("Accept"))
		})
	}
}
