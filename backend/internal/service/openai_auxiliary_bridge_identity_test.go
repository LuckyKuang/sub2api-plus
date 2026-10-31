//go:build unit

package service

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/LuckyKuang/sub2api-plus/internal/codexcli"
	"github.com/stretchr/testify/require"
)

// docs/OUTBOUND_IDENTITY.md: bridge permissions select only the transport.
// Public/admin discovery and the Codex manifest must independently preserve
// account > global > compiled identity, including invalid candidate fallthrough.
// These literal expectations describe the contract, not resolver output.
func TestOpenAIModelDiscoveryBridgeIdentitySourceMatrix(t *testing.T) {
	const accountUA = "codex_vscode/0.165.0 (Ubuntu 24.04; aarch64) vscode"
	const globalUA = "codex-tui/0.165.0 (Ubuntu 24.04; x86_64) screen-256color"
	const defaultUA = "codex_cli_rs/0.165.0 (Ubuntu 24.04; x86_64) xterm-256color"
	for _, source := range []struct {
		name, account, global, wantUA, wantOriginator string
	}{
		{"account", accountUA, globalUA, accountUA, "codex_vscode"},
		{"global", "", globalUA, globalUA, "codex-tui"},
		{"invalid_account", "curl/8.0", globalUA, globalUA, "codex-tui"},
		{"invalid_global", "curl/8.0", "untrusted/9.9", defaultUA, "codex_cli_rs"},
		{"compiled", "", "", defaultUA, "codex_cli_rs"},
	} {
		for _, switches := range []struct {
			name, global string
			account      bool
			wantHops     int
		}{
			{"global_disabled", "false", true, 0},
			{"account_disabled", "true", false, 0},
			{"bridge_enabled", "true", true, 1},
		} {
			for _, accountType := range []string{AccountTypeOAuth, AccountTypeAPIKey} {
				for _, public := range []bool{false, true} {
					t.Run(fmt.Sprintf("%s/%s/%s/public=%t", source.name, switches.name, accountType, public), func(t *testing.T) {
						captured := make(chan *http.Request, 2)
						server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
							captured <- r.Clone(context.Background())
							w.Header().Set("Content-Type", "application/json")
							if accountType == AccountTypeAPIKey && public {
								_, _ = w.Write([]byte(`{"data":[{"id":"gpt-5.4"}]}`))
							} else {
								_, _ = w.Write([]byte(`{"models":[{"slug":"gpt-5.4"}]}`))
							}
						}))
						t.Cleanup(server.Close)
						originalURL := chatgptCodexModelsURL
						chatgptCodexModelsURL = server.URL + "/backend-api/codex/models"
						t.Cleanup(func() { chatgptCodexModelsURL = originalURL })
						owner := enabledCodexCLIExecutionAccount(411, "owner-access", "owner-workspace")
						owner.Type = accountType
						owner.Credentials["user_agent"] = source.account
						owner.Credentials["api_key"] = "owner-key"
						owner.Credentials["base_url"] = "https://models.example/v1"
						owner.Credentials[CodexCLIConfigCredentialKey].(map[string]any)["enabled"] = switches.account
						owner.Credentials[credKeyHeaderOverrideEnabled] = true
						owner.Credentials[credKeyHeaderOverrides] = map[string]any{
							"User-Agent": "untrusted/9.9", "Originator": "untrusted", "Version": "9.9",
							"X-Discovery-Identity-Test": "allowed-override",
						}
						settings := &SettingService{settingRepo: &openAIIdentitySettingRepoStub{values: map[string]string{
							SettingKeyOpenAICodexUserAgent: source.global, SettingKeyCodexCLIBridgeEnabled: switches.global,
						}}}
						// API-key requests retain their normal builder/transport path;
						// the test transport redirects only the final dial to our server.
						gateway := newCodexModelsAPIKeyTestService(&codexModelsHTTPUpstreamStub{do: func(r *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
							local := r.Clone(r.Context())
							local.URL.Scheme = "http"
							local.URL.Host = server.Listener.Addr().String()
							return server.Client().Do(local)
						}})
						gateway.settingService = settings
						hops := 0
						gateway.codexCLIBridgeRuntime = codexCLIBridgeTransportStub{status: codexCLIBridgeReadyFixture(), bridge: func(ctx context.Context, _ codexcli.Status, r *http.Request, forward func(context.Context, *http.Request) (*http.Response, error)) (*http.Response, error) {
							hops++
							return forward(ctx, r)
						}}
						var err error
						if public {
							_, err = gateway.FetchOpenAIModelsList(context.Background(), owner)
						} else {
							_, err = gateway.FetchCodexModelsManifest(context.Background(), owner, "9.9.9", "")
						}
						require.NoError(t, err)
						require.Equal(t, switches.wantHops, hops)
						require.Len(t, captured, 1, "transport permission must not replay discovery")
						wire := <-captured
						require.Equal(t, http.MethodGet, wire.Method)
						require.Equal(t, []string{source.wantUA}, wire.Header.Values("User-Agent"))
						require.Equal(t, []string{source.wantOriginator}, wire.Header.Values("Originator"))
						require.Equal(t, []string{"0.165.0"}, wire.Header.Values("Version"))
						if accountType == AccountTypeOAuth {
							require.Equal(t, "/backend-api/codex/models", wire.URL.Path)
							require.Equal(t, "Bearer owner-access", wire.Header.Get("Authorization"))
							require.Equal(t, "owner-workspace", wire.Header.Get("Chatgpt-Account-Id"))
						} else {
							require.Equal(t, "/v1/models", wire.URL.Path)
							require.Equal(t, "Bearer owner-key", wire.Header.Get("Authorization"))
							require.Empty(t, wire.Header.Get("Chatgpt-Account-Id"))
							require.Equal(t, "allowed-override", wire.Header.Get("X-Discovery-Identity-Test"), "a permitted field proves the generic override path executes")
						}
						if public && accountType == AccountTypeAPIKey {
							require.Empty(t, wire.URL.Query().Get("client_version"))
						} else {
							require.Equal(t, "0.165.0", wire.URL.Query().Get("client_version"), "catalog query must agree with the selected identity")
						}
					})
				}
			}
		}
	}
}
