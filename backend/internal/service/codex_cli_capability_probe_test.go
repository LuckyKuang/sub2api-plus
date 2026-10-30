//go:build unit

package service

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/LuckyKuang/sub2api-plus/internal/codexcli"
	"github.com/LuckyKuang/sub2api-plus/internal/config"
	"github.com/LuckyKuang/sub2api-plus/internal/pkg/openai_compat"
	"github.com/stretchr/testify/require"
)

// docs/CODEX_CLI.md requires probes to use the same explicit bridge permissions
// as gateway traffic, preserving the acquired owner and never falling back on
// bridge failure. Invoke the actual automatic capability probe, not its sender.
func TestCodexCLICapabilityProbePermissionsAndFailures(t *testing.T) {
	for _, tc := range []struct {
		name                 string
		global, account      bool
		failure              string
		wantHops, wantWrites int
		wantPersist          bool
	}{
		{"native-global-off", false, true, "", 0, 1, true},
		{"bridge-enabled", true, true, "", 1, 1, true},
		{"native-account-off", true, false, "", 0, 1, true},
		{"runtime-unavailable", true, true, "runtime", 0, 0, false},
		{"setting-unavailable", true, true, "setting", 0, 0, false},
		{"bridge-before-send", true, true, "before-send", 1, 0, false},
		{"bridge-after-send", true, true, "after-send", 1, 1, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			account := newResponsesProbeAccount(805)
			account.Credentials[CodexCLIConfigCredentialKey] = map[string]any{"enabled": tc.account}
			account.Credentials["model_mapping"] = map[string]any{"client": "gpt-probe"}
			account.Credentials["user_agent"] = "codex_vscode/0.164.0 (Mac OS X 15.1.0; arm64) iTerm.app"
			proxyID := int64(8)
			account.ProxyID = &proxyID
			account.Proxy = &Proxy{ID: proxyID, Protocol: "http", Host: "probe-proxy.example", Port: 8080}
			repo := &responsesProbeAccountRepo{snapshotUpdateAccountRepo: snapshotUpdateAccountRepo{
				stubOpenAIAccountRepo: stubOpenAIAccountRepo{accounts: []Account{account}},
				updateExtraCalls:      make(chan map[string]any, 1),
			}}
			global := &codexCLIBridgeSettingRepo{
				// The explicit manual version pin preserves the account's selected
				// family/fingerprint under docs/OUTBOUND_IDENTITY.md.
				SettingRepository: &openAIIdentitySettingRepoStub{values: map[string]string{SettingKeyOpenAICodexClientVersion: "0.164.0"}},
				values:            map[string]string{SettingKeyCodexCLIBridgeEnabled: "false"},
			}
			if tc.global {
				global.values[SettingKeyCodexCLIBridgeEnabled] = "true"
			}
			if tc.failure == "setting" {
				global.readErr = errors.New("setting unavailable")
			}
			settings := &SettingService{settingRepo: global}
			hops, writes := 0, 0
			upstream := &codexModelsHTTPUpstreamStub{do: func(r *http.Request, proxy string, id int64, concurrency int) (*http.Response, error) {
				writes++
				require.Equal(t, http.MethodPost, r.Method)
				require.Equal(t, "https://compat-upstream.example/v1/responses", r.URL.String())
				require.Equal(t, "http://probe-proxy.example:8080", proxy)
				require.Equal(t, int64(805), id)
				require.Equal(t, 1, concurrency)
				require.Equal(t, "Bearer sk-test", r.Header.Get("Authorization"))
				require.Empty(t, r.Header.Get("Chatgpt-Account-Id"))
				require.Equal(t, "codex_vscode/0.164.0 (Mac OS X 15.1.0; arm64) iTerm.app", r.UserAgent())
				require.Equal(t, "codex_vscode", r.Header.Get("Originator"))
				require.Equal(t, "0.164.0", r.Header.Get("Version"))
				body, err := io.ReadAll(r.Body)
				require.NoError(t, err)
				require.JSONEq(t, `{"model":"gpt-probe","input":[{"role":"user","content":[{"type":"input_text","text":"Call the probe_ping function with ok=true to acknowledge readiness. You must use the tool."}]}],"tools":[{"type":"function","name":"probe_ping","description":"Capability probe. Call to acknowledge.","parameters":{"type":"object","properties":{"ok":{"type":"boolean"}},"required":["ok"]}}],"tool_choice":"required","max_output_tokens":512,"stream":false}`, string(body))
				return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"status":"completed","output":[{"type":"function_call","name":"probe_ping"}]}`))}, nil
			}}
			status := codexCLIBridgeReadyFixture()
			if tc.failure == "runtime" {
				status.ExecutionAvailable = false
			}
			gateway := &OpenAIGatewayService{accountRepo: repo, settingService: settings,
				codexCLIBridgeRuntime: codexCLIBridgeTransportStub{status: status, bridge: func(ctx context.Context, _ codexcli.Status, r *http.Request, forward func(context.Context, *http.Request) (*http.Response, error)) (*http.Response, error) {
					hops++
					if tc.failure == "before-send" {
						return nil, &codexcli.BridgeError{Code: "proxy_failed"}
					}
					response, err := forward(ctx, r)
					if tc.failure == "after-send" {
						require.NoError(t, response.Body.Close())
						return nil, &codexcli.BridgeError{Code: "proxy_failed", RequestSent: true}
					}
					return response, err
				}},
			}
			probe := &AccountTestService{accountRepo: repo, settingService: settings, openaiGatewayService: gateway, httpUpstream: upstream, cfg: &config.Config{}}
			probe.ProbeOpenAIAPIKeyResponsesSupport(context.Background(), account.ID)
			require.Equal(t, tc.wantHops, hops)
			require.Equal(t, tc.wantWrites, writes)
			if tc.wantPersist {
				require.Equal(t, 1, repo.casCalls)
				require.Len(t, repo.updateExtraCalls, 1)
				require.Equal(t, true, (<-repo.updateExtraCalls)[openai_compat.ExtraKeyResponsesSupported])
			} else {
				require.Zero(t, repo.casCalls, "transport failures cannot become capability evidence")
				require.Empty(t, repo.updateExtraCalls)
			}
			require.Zero(t, repo.unconditionalWrites)
		})
	}
}
