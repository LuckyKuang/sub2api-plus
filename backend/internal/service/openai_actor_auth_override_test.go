//go:build unit

package service

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// Official Codex c9b4267c37 model-provider-info/src/lib.rs identifies this as
// authentication (uses_openai_actor_authorization). The account-override
// contract reserves authentication for the credential owner, including a
// previously stored override that bypassed save validation.
func TestOpenAIActorAuthorizationOverrideRejected(t *testing.T) {
	for _, name := range []string{"X-OpenAI-Actor-Authorization", "x-openai-actor-authorization", "X-OPENAI-ACTOR-AUTHORIZATION"} {
		for _, value := range []string{"", "Bearer foreign-actor"} {
			for _, enabled := range []bool{false, true} {
				t.Run(name+"/blank="+strconvBool(value == "")+"/enabled="+strconvBool(enabled), func(t *testing.T) {
					err := NormalizeHeaderOverrideCredentials(map[string]any{
						credKeyHeaderOverrideEnabled: enabled,
						credKeyHeaderOverrides:       map[string]any{name: value},
					})
					requireApplicationErrorReason(t, err, "INVALID_HEADER_OVERRIDE")
				})
			}
		}
	}
}

func TestOpenAIActorAuthorizationCannotReachProvider(t *testing.T) {
	for _, kind := range []string{AccountTypeOAuth, AccountTypeAPIKey} {
		for _, stringMap := range []bool{false, true} {
			for _, path := range []string{"http", "passthrough", "websocket"} {
				t.Run(kind+"/string-map="+strconvBool(stringMap)+"/"+path, func(t *testing.T) {
					observed := make(chan http.Header, 1)
					server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						observed <- r.Header.Clone()
						w.Header().Set("Content-Type", "text/event-stream")
						_, _ = io.WriteString(w, "data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_auth\",\"output\":[],\"usage\":{\"input_tokens\":1,\"output_tokens\":1}}}\n\n")
					}))
					defer server.Close()
					account := newOpenAIOAuthNamespaceTestAccount()
					wantAuth := "Bearer oauth-token"
					if kind == AccountTypeAPIKey {
						account = newOpenAIRejectedFieldTestAccount()
						wantAuth = "Bearer sk-test"
					}
					account.Extra = map[string]any{"openai_passthrough": path == "passthrough"}
					account.Credentials[credKeyHeaderOverrideEnabled] = true
					var overrides any = map[string]any{"X-OPENAI-ACTOR-AUTHORIZATION": "Bearer foreign-actor", "X-Route": "retained"}
					if stringMap {
						overrides = map[string]string{"x-openai-actor-authorization": "Bearer foreign-actor", "X-Route": "retained"}
					}
					account.Credentials[credKeyHeaderOverrides] = overrides
					svc := newOpenAIRejectedFieldTestService(nil)
					svc.httpUpstream = &codexModelsHTTPUpstreamStub{do: func(req *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
						local := req.Clone(req.Context())
						local.URL.Scheme, local.URL.Host, local.Host = "http", server.Listener.Addr().String(), server.Listener.Addr().String()
						return server.Client().Do(local)
					}}
					body := []byte(`{"model":"gpt-5.4","input":"hello","stream":true}`)
					c := newOpenAIRejectedFieldTestContext(body)
					c.Request.Header.Set("X-OpenAI-Actor-Authorization", "Bearer inbound-actor")
					if path == "websocket" {
						headers, _, err := svc.buildOpenAIWSHeaders(t.Context(), c, account, strings.TrimPrefix(wantAuth, "Bearer "), OpenAIWSProtocolDecision{Transport: OpenAIUpstreamTransportResponsesWebsocketV2}, false, "", "", "", "gpt-5.4", "")
						require.NoError(t, err)
						// A rejected upgrade still captures the actual HTTP handshake.
						ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
						defer cancel()
						_, _, _, err = newDefaultOpenAIWSClientDialer().Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http")+"/v1/responses", headers, "")
						require.Error(t, err, "fixture intentionally responds with SSE, not an upgrade")
					} else {
						_, err := svc.Forward(t.Context(), c, account, body)
						require.NoError(t, err)
					}
					select {
					case wire := <-observed:
						require.Empty(t, wire.Values("X-OpenAI-Actor-Authorization"), "generic overrides and inbound auth cannot add a second credential")
						require.Equal(t, []string{wantAuth}, wire.Values("Authorization"))
						require.Equal(t, "codex_cli_rs/0.167.0 (Ubuntu 24.04; x86_64) xterm-256color", wire.Get("User-Agent"))
						require.Equal(t, "codex_cli_rs", wire.Get("Originator"))
						require.Equal(t, "0.167.0", wire.Get("Version"))
						if kind == AccountTypeAPIKey {
							require.Equal(t, "retained", wire.Get("X-Route"))
						}
					default:
						t.Fatal("expected exactly one provider request")
					}
					require.Empty(t, observed)
				})
			}
		}
	}
}
