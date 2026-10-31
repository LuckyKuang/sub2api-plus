//go:build unit

package service

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync/atomic"
	"testing"

	"github.com/LuckyKuang/sub2api-plus/internal/codexcli"
	"github.com/LuckyKuang/sub2api-plus/internal/pkg/outboundidentity"
	"github.com/stretchr/testify/require"
)

// docs/OUTBOUND_IDENTITY.md: bridge permissions select the transport only.
// These less common public endpoints must retain account > global > compiled
// identity across enable, global disable, re-enable and account disable. Capture
// actual HTTP headers after the production request builders and send boundary;
// the bridge fixture replaces only the already covered official local CLI hop.
func TestOpenAIBridgeAuxiliaryInferenceIdentityTransitions(t *testing.T) {
	identities := []struct {
		name, accountUA, globalUA, wantUA, wantOriginator string
	}{
		{"account override", "codex_vscode/0.165.0 (Windows 11; aarch64) vscode", "codex-tui/0.165.0 (Ubuntu 24.04; x86_64) screen", "codex_vscode/0.165.0 (Windows 11; aarch64) vscode", "codex_vscode"},
		{"empty account inherits global", "", "codex-tui/0.165.0 (Ubuntu 24.04; x86_64) screen", "codex-tui/0.165.0 (Ubuntu 24.04; x86_64) screen", "codex-tui"},
		{"invalid account inherits global", "curl/8.0", "codex-tui/0.165.0 (Ubuntu 24.04; x86_64) screen", "codex-tui/0.165.0 (Ubuntu 24.04; x86_64) screen", "codex-tui"},
		{"invalid candidates inherit compiled", "curl/8.0", "invalid", "codex_cli_rs/0.165.0 (Ubuntu 24.04; x86_64) xterm-256color", "codex_cli_rs"},
	}
	for _, endpoint := range []string{"/v1/embeddings", "/v1/responses/input_tokens", "/v1/messages/count_tokens", "/v1/responses/compact"} {
		for _, accountType := range []string{AccountTypeOAuth, AccountTypeAPIKey, AccountTypeSetupToken} {
			if endpoint == "/v1/embeddings" && accountType != AccountTypeAPIKey {
				continue // Embeddings is an API-key capability.
			}
			for _, identity := range identities {
				t.Run(endpoint+"/"+accountType+"/"+identity.name, func(t *testing.T) {
					account := newOpenAIOAuthNamespaceTestAccount()
					wantAuth := "Bearer oauth-token"
					if accountType == AccountTypeAPIKey {
						account = newOpenAIRejectedFieldTestAccount()
						account.Credentials["base_url"] = "https://api.openai.com"
						wantAuth = "Bearer sk-test"
					}
					account.Type = accountType
					account.Credentials["user_agent"] = identity.accountUA
					account.Credentials[CodexCLIConfigCredentialKey] = map[string]any{"enabled": true, "config_toml": DefaultCodexCLIConfigTOML}
					account.Credentials[credKeyHeaderOverrideEnabled] = true
					account.Credentials[credKeyHeaderOverrides] = map[string]any{"user-agent": "injected/999", "originator": "injected", "version": "999.0.0"}
					global := &codexBridgeSwitchRepo{openAIIdentitySettingRepoStub: &openAIIdentitySettingRepoStub{values: map[string]string{SettingKeyOpenAICodexUserAgent: identity.globalUA}}}
					flag := &atomic.Bool{}
					rows := &codexBridgeHotAccountRepo{owners: map[int64]*Account{account.ID: account}, flags: map[int64]*atomic.Bool{account.ID: flag}}
					type wireObservation struct {
						header http.Header
						path   string
					}
					observations := make(chan wireObservation, 16)
					provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						_, _ = io.Copy(io.Discard, r.Body)
						select {
						case observations <- wireObservation{header: r.Header.Clone(), path: r.URL.Path}:
						case <-r.Context().Done():
							return
						}
						w.Header().Set("Content-Type", "application/json")
						switch endpoint {
						case "/v1/embeddings":
							_, _ = io.WriteString(w, `{"object":"list","data":[{"object":"embedding","index":0,"embedding":[0.1]}],"usage":{"prompt_tokens":3,"total_tokens":3}}`)
						case "/v1/responses/compact":
							_, _ = io.WriteString(w, `{"id":"cmp_test","object":"response.compaction","output":[],"usage":{"input_tokens":3,"output_tokens":1}}`)
						default:
							_, _ = io.WriteString(w, `{"input_tokens":3}`)
						}
					}))
					defer provider.Close()
					localURL, err := url.Parse(provider.URL)
					require.NoError(t, err)
					writes, hops := 0, 0
					svc := newOpenAIRejectedFieldTestService(nil)
					svc.accountRepo, svc.settingService = rows, &SettingService{settingRepo: global}
					svc.httpUpstream = &codexModelsHTTPUpstreamStub{do: func(req *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
						writes++
						wire := req.Clone(req.Context())
						wire.URL.Scheme, wire.URL.Host, wire.Host = localURL.Scheme, localURL.Host, localURL.Host
						return http.DefaultClient.Do(wire)
					}}
					svc.codexCLIBridgeRuntime = codexCLIBridgeTransportStub{status: codexCLIBridgeReadyFixture(), bridge: func(ctx context.Context, _ codexcli.Status, req *http.Request, forward func(context.Context, *http.Request) (*http.Response, error)) (*http.Response, error) {
						hops++
						return forward(ctx, req)
					}}
					for step, permission := range []struct{ global, account, bridge bool }{{true, true, true}, {false, true, false}, {true, true, true}, {true, false, false}, {false, false, false}} {
						global.enabled.Store(permission.global)
						flag.Store(permission.account)
						body := []byte(`{"model":"gpt-5.4","input":"hello","stream":false}`)
						if endpoint == "/v1/messages/count_tokens" {
							body = []byte(`{"model":"gpt-5.4","messages":[{"role":"user","content":"hello"}]}`)
						}
						c := newOpenAIIdentityContractContaminatedContext(body)
						c.Request.URL.Path = endpoint
						ctx := outboundidentity.WithResolver(t.Context(), svc.settingService.resolveOutboundIdentityKey)
						before := hops
						switch endpoint {
						case "/v1/embeddings":
							_, err = svc.ForwardEmbeddings(ctx, c, account, body, "")
						case "/v1/responses/input_tokens":
							err = svc.ForwardResponsesInputTokens(ctx, c, account, body)
						case "/v1/messages/count_tokens":
							err = svc.ForwardCountTokensAsAnthropic(ctx, c, account, body, "")
						default:
							_, err = svc.Forward(ctx, c, account, body)
						}
						require.NoError(t, err, "step %d", step)
						require.Equal(t, step+1, writes, "one provider write per accepted operation")
						wantHops := 0
						if permission.bridge && accountType != AccountTypeSetupToken {
							wantHops = 1
						}
						require.Equal(t, wantHops, hops-before, "step %d", step)
						observed := <-observations
						wantPath := endpoint
						if endpoint == "/v1/messages/count_tokens" {
							wantPath = "/v1/responses/input_tokens"
						} else if endpoint == "/v1/responses/compact" && accountType != AccountTypeAPIKey {
							wantPath = "/backend-api/codex/responses/compact"
						}
						require.Equal(t, wantPath, observed.path)
						require.Equal(t, []string{identity.wantUA}, observed.header.Values("User-Agent"))
						require.Equal(t, []string{identity.wantOriginator}, observed.header.Values("Originator"))
						require.Equal(t, []string{"0.165.0"}, observed.header.Values("Version"))
						require.Equal(t, wantAuth, observed.header.Get("Authorization"))
					}
				})
			}
		}
	}
}
