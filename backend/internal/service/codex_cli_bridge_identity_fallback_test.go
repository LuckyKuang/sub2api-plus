//go:build unit

package service

import (
	"context"
	"io"
	"net/http"
	"strconv"
	"strings"
	"testing"

	"github.com/LuckyKuang/sub2api-plus/internal/codexcli"
	"github.com/LuckyKuang/sub2api-plus/internal/config"
	"github.com/LuckyKuang/sub2api-plus/internal/pkg/openai_compat"
	"github.com/stretchr/testify/require"
)

// docs/OUTBOUND_IDENTITY.md: the bridge switches select transport only. Account
// > global > compiled identity precedence, including invalid-candidate fallback,
// must survive the complete request builder and final provider transport.
func TestCodexCLIBridgeIdentityFallbackAcrossGatewayAndAutomaticProbe(t *testing.T) {
	const accountUA = "codex_vscode/0.165.0 (Mac OS X 15.1.0; arm64) iTerm.app"
	const globalUA = "codex-tui/0.165.0 (Ubuntu 22.4.0; x86_64) screen-256color"
	const defaultUA = "codex_cli_rs/0.165.0 (Ubuntu 24.04; x86_64) xterm-256color"
	for _, source := range []struct {
		name, accountUA, globalUA, wantUA, wantOriginator string
	}{
		{"account-over-global", accountUA, globalUA, accountUA, "codex_vscode"},
		{"account-over-invalid-global", accountUA, "untrusted-global/1", accountUA, "codex_vscode"},
		{"empty-account-inherits-global", "", globalUA, globalUA, "codex-tui"},
		{"invalid-account-inherits-global", "untrusted-account/1", globalUA, globalUA, "codex-tui"},
		{"empty-candidates-use-default", "", "", defaultUA, "codex_cli_rs"},
		{"invalid-candidates-use-default", "untrusted-account/1", "untrusted-global/1", defaultUA, "codex_cli_rs"},
	} {
		for _, transport := range []struct {
			name            string
			global, account bool
			wantHops        int
		}{
			{"global-disabled", false, true, 0},
			{"account-disabled", true, false, 0},
			{"bridge-enabled", true, true, 1},
		} {
			for _, path := range []string{"oauth-responses", "apikey-responses", "automatic-capability-probe"} {
				t.Run(source.name+"/"+transport.name+"/"+path, func(t *testing.T) {
					owner := enabledCodexCLIExecutionAccount(2901, "acquired-access", "acquired-workspace")
					token, wantURL := "acquired-access", "https://chatgpt.com/backend-api/codex/responses"
					if path != "oauth-responses" {
						account := newResponsesProbeAccount(2901)
						owner = &account
						token, wantURL = "sk-test", "https://compat-upstream.example/v1/responses"
					}
					owner.Credentials["user_agent"] = source.accountUA
					owner.Credentials[CodexCLIConfigCredentialKey] = map[string]any{"enabled": transport.account}
					owner.Credentials[credKeyHeaderOverrideEnabled] = true
					owner.Credentials[credKeyHeaderOverrides] = map[string]any{
						"User-Agent": "codex_cli_rs/9.9.9 (injected; arm64) forged-terminal",
						"Originator": "injected-originator", "Version": "9.9.9",
						"X-Bridge-Identity-Test": "enabled-override",
					}
					repo := &responsesProbeAccountRepo{snapshotUpdateAccountRepo: snapshotUpdateAccountRepo{
						stubOpenAIAccountRepo: stubOpenAIAccountRepo{accounts: []Account{*owner}},
						updateExtraCalls:      make(chan map[string]any, 1),
					}}
					settings := &SettingService{settingRepo: &openAIIdentitySettingRepoStub{values: map[string]string{
						SettingKeyCodexCLIBridgeEnabled: strconv.FormatBool(transport.global),
						SettingKeyOpenAICodexUserAgent:  source.globalUA,
					}}}
					writes, hops := 0, 0
					upstream := &codexModelsHTTPUpstreamStub{do: func(request *http.Request, _ string, id int64, _ int) (*http.Response, error) {
						writes++
						// The sender preserves raw wire casing for custom headers.
						// Observe all case-insensitive matches, including duplicates,
						// instead of Header.Get's canonical-map-key lookup.
						headerValues := func(want string) []string {
							var values []string
							for name, entries := range request.Header {
								if strings.EqualFold(name, want) {
									values = append(values, entries...)
								}
							}
							return values
						}
						require.Equal(t, int64(2901), id)
						require.Equal(t, wantURL, request.URL.String())
						require.Equal(t, "Bearer "+token, request.Header.Get("Authorization"))
						require.Equal(t, []string{source.wantUA}, headerValues("User-Agent"))
						require.Equal(t, []string{source.wantOriginator}, headerValues("Originator"))
						require.Equal(t, []string{"0.165.0"}, headerValues("Version"))
						if path == "oauth-responses" {
							require.Equal(t, "acquired-workspace", request.Header.Get("Chatgpt-Account-Id"))
						} else {
							require.Empty(t, request.Header.Get("Chatgpt-Account-Id"))
							require.Equal(t, []string{"enabled-override"}, headerValues("X-Bridge-Identity-Test"), "a permitted header proves the API-key override path is active")
						}
						return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"status":"completed","output":[{"type":"function_call","name":"probe_ping"}]}`))}, nil
					}}
					gateway := &OpenAIGatewayService{cfg: &config.Config{}, accountRepo: repo, settingService: settings, httpUpstream: upstream,
						codexCLIBridgeRuntime: codexCLIBridgeTransportStub{status: codexCLIBridgeReadyFixture(), bridge: func(ctx context.Context, _ codexcli.Status, request *http.Request, forward func(context.Context, *http.Request) (*http.Response, error)) (*http.Response, error) {
							hops++
							return forward(ctx, request)
						}},
					}
					if path == "automatic-capability-probe" {
						probe := &AccountTestService{cfg: gateway.cfg, accountRepo: repo, settingService: settings, openaiGatewayService: gateway, httpUpstream: upstream}
						probe.ProbeOpenAIAPIKeyResponsesSupport(context.Background(), owner.ID)
						require.Equal(t, 1, repo.casCalls)
						require.Len(t, repo.updateExtraCalls, 1)
						require.Equal(t, true, (<-repo.updateExtraCalls)[openai_compat.ExtraKeyResponsesSupported])
						require.Zero(t, repo.unconditionalWrites)
					} else {
						c, _ := newCodexCLIExecutionContext()
						c.Request.Header.Set("User-Agent", "codex_cli_rs/9.9.9 (inbound; arm64) forged-terminal")
						c.Request.Header.Set("Originator", "codex_cli_rs")
						c.Request.Header.Set("Version", "9.9.9")
						ctx := withRequestCredentialOwner(WithOutboundIdentityScope(c.Request.Context(), c), owner, token)
						request, err := gateway.buildUpstreamRequest(ctx, c, owner, []byte(`{"model":"gpt-6-astra","input":"identity test"}`), token, false, "", true)
						require.NoError(t, err)
						response, err := gateway.doOpenAIUpstream(request, "", owner)
						require.NoError(t, err)
						require.NoError(t, response.Body.Close())
					}
					require.Equal(t, transport.wantHops, hops)
					require.Equal(t, 1, writes, "a transport choice must not replay the provider request")
				})
			}
		}
	}
}
