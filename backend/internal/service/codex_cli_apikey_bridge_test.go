//go:build unit

package service

import (
	"context"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/LuckyKuang/sub2api-plus/internal/codexcli"
	"github.com/LuckyKuang/sub2api-plus/internal/config"
	"github.com/stretchr/testify/require"
)

func codexBridgeAPIKeyFixture() *Account {
	return &Account{ID: 801, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Concurrency: 1, Credentials: map[string]any{
		"api_key": "acquired-api-key", "base_url": "https://compatible.example/v1",
		"user_agent":                "codex_cli_rs/0.165.0 (Ubuntu 24.04; x86_64) xterm-256color",
		CodexCLIConfigCredentialKey: map[string]any{"enabled": true, "config_toml": DefaultCodexCLIConfigTOML},
	}}
}

func TestCodexCLIAPIKeyConfigurationDoesNotEnableOAuthTasks(t *testing.T) {
	owner := codexBridgeAPIKeyFixture()
	require.NoError(t, ValidateCodexCLIAccount(owner))
	credentials := map[string]any{CodexCLIConfigCredentialKey: map[string]any{"enabled": false, "config_toml": ""}}
	require.NoError(t, NormalizeCodexCLIAccountCredentials(PlatformOpenAI, AccountTypeAPIKey, credentials))
	require.Equal(t, "sandbox_mode = \"danger-full-access\"\napproval_policy = \"never\"\ncli_auth_credentials_store = \"file\"\n", credentials[CodexCLIConfigCredentialKey].(map[string]any)["config_toml"])
	require.False(t, CodexCLITaskAccountEnabled(owner))
	_, err := BuildCodexCLIAuthJSON(owner)
	require.Error(t, err, "API Key bridge must never materialize OAuth task auth.json")
	view, err := CodexCLIAccountViewFromAccount(owner)
	require.NoError(t, err)
	require.Equal(t, "apikey", view.Auth.AuthMode)
	require.Empty(t, view.Auth.Tokens)
	require.Equal(t, "http_bridge_only", view.Availability.Code)
	require.NotEmpty(t, view.ExpectedOwner)
	require.Empty(t, view.OAuthCredentialOwner)
	changed := snapshotRequestCredentialAccount(owner)
	changed.Credentials["api_key"] = "new-key"
	require.NotEqual(t, view.ExpectedOwner, CodexCLIAccountOwner(changed))
	changed = snapshotRequestCredentialAccount(owner)
	changed.Credentials["base_url"] = "https://different.example"
	require.NotEqual(t, view.ExpectedOwner, CodexCLIAccountOwner(changed))
}

// Existing API-key authentication, custom routing, body and trusted identity
// remain identical across both explicit permissions in either direction.
func TestCodexCLIAPIKeyBridgeHotSwitchGatewayProbeAndWSGate(t *testing.T) {
	for _, path := range []string{"gateway", "probe"} {
		t.Run(path, func(t *testing.T) {
			owner := codexBridgeAPIKeyFixture()
			global := &codexBridgeSwitchRepo{openAIIdentitySettingRepoStub: &openAIIdentitySettingRepoStub{values: map[string]string{}}}
			settings := &SettingService{settingRepo: global}
			settings.storeCodexTransportSettings(CodexTransportSettings{WSEnabled: true})
			flag := &atomic.Bool{}
			rows := &codexBridgeHotAccountRepo{owners: map[int64]*Account{owner.ID: owner}, flags: map[int64]*atomic.Bool{owner.ID: flag}}
			var writes, hops int
			upstream := &codexModelsHTTPUpstreamStub{do: func(req *http.Request, _ string, id int64, _ int) (*http.Response, error) {
				writes++
				require.Equal(t, int64(801), id)
				require.Equal(t, "https://compatible.example/v1/responses?trusted=1", req.URL.String())
				require.Equal(t, "Bearer acquired-api-key", req.Header.Get("Authorization"))
				require.Empty(t, req.Header.Get("Chatgpt-Account-Id"), "OAuth workspace must not leak into API Key requests")
				require.Equal(t, "codex_cli_rs/0.165.0 (Ubuntu 24.04; x86_64) xterm-256color", req.Header.Get("User-Agent"))
				require.Equal(t, "codex_cli_rs", req.Header.Get("Originator"))
				require.Equal(t, "0.165.0", req.Header.Get("Version"))
				got, err := io.ReadAll(req.Body)
				require.NoError(t, err)
				require.Equal(t, `{"model":"custom-model","input":"opaque","tools":[{"type":"function","name":"caller"}]}`, string(got))
				return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("accepted"))}, nil
			}}
			cfg := &config.Config{}
			cfg.Gateway.OpenAIWS.Enabled, cfg.Gateway.OpenAIWS.APIKeyEnabled = true, true
			svc := &OpenAIGatewayService{cfg: cfg, accountRepo: rows, settingService: settings, httpUpstream: upstream,
				codexCLIBridgeRuntime: codexCLIBridgeTransportStub{status: codexCLIBridgeReadyFixture(), bridge: func(ctx context.Context, _ codexcli.Status, req *http.Request, forward func(context.Context, *http.Request) (*http.Response, error)) (*http.Response, error) {
					hops++
					return forward(ctx, req)
				}},
			}
			probe := &AccountTestService{accountRepo: rows, openaiGatewayService: svc, settingService: settings, httpUpstream: upstream}
			ctx := withRequestCredentialOwner(WithOutboundIdentityScope(context.Background(), nil), owner, "acquired-api-key")
			for step, state := range []struct{ global, account, bridge bool }{{false, true, false}, {true, true, true}, {true, false, false}, {true, true, true}, {false, true, false}, {true, true, true}} {
				global.enabled.Store(state.global)
				flag.Store(state.account)
				require.Equal(t, !state.bridge, svc.codexUpstreamWSAllowed(ctx, owner))
				req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://compatible.example/v1/responses?trusted=1", strings.NewReader(`{"model":"custom-model","input":"opaque","tools":[{"type":"function","name":"caller"}]}`))
				require.NoError(t, err)
				req.Header.Set("Authorization", "Bearer acquired-api-key")
				req.Header.Set("User-Agent", "forged-inbound/999")
				// Model the existing request builder before entering the prepared
				// transport boundary; the CLI hop must preserve this native triple.
				svc.applyOpenAIOutboundIdentity(req.Context(), owner, req.Header, true)
				before := hops
				var resp *http.Response
				if path == "gateway" {
					resp, err = svc.doOpenAIUpstream(req, "", owner)
				} else {
					resp, err = probe.doOpenAIAccountTestUpstream(req, "", owner, false)
				}
				require.NoError(t, err)
				require.NoError(t, resp.Body.Close())
				if state.bridge {
					require.Equal(t, before+1, hops)
				} else {
					require.Equal(t, before, hops)
				}
				require.Equal(t, step+1, writes, "transport switch cannot replay")
			}
		})
	}
}
