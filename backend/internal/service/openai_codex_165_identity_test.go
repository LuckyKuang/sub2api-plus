//go:build unit

package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCodex165ManualUpgradePreservesSourceAndFingerprintOnWire(t *testing.T) {
	// Oracle: official stable rust-v0.165.0, peeled 360319d8dee62d87e2d59fc0c21805d79519cfed;
	// docs/protocols/CODEX_CLIENT_PROFILES.md source matrix and version-only contract.
	for _, kind := range []string{AccountTypeOAuth, AccountTypeAPIKey} {
		for _, tc := range []struct{ name, accountUA, globalUA, pinnedVersion, source, ua, originator, version string }{
			{"compiled", "", "", "", "compiled_default", "codex_cli_rs/0.165.0 (Ubuntu 24.04; x86_64) xterm-256color", "codex_cli_rs", "0.165.0"},
			{"account before global", "codex_vscode/0.164.0 (Windows 11; aarch64) WindowsTerminal", "codex_cli_rs/0.163.0 (Ubuntu 22.04; x86_64) tmux", "", "account", "codex_vscode/0.165.0 (Windows 11; aarch64) WindowsTerminal", "codex_vscode", "0.165.0"},
			{"invalid account falls to global", "foreign/1.0", "codex_cli_rs/0.163.0 (Ubuntu 22.04; x86_64) tmux", "", "global", "codex_cli_rs/0.165.0 (Ubuntu 22.04; x86_64) tmux", "codex_cli_rs", "0.165.0"},
			{"invalid both fall to compiled", "foreign/1.0", "foreign/2.0", "", "compiled_default", "codex_cli_rs/0.165.0 (Ubuntu 24.04; x86_64) xterm-256color", "codex_cli_rs", "0.165.0"},
			{"explicit previous release pin", "codex_vscode/0.163.0 (Windows 11; aarch64) WindowsTerminal", "", "0.164.0", "account", "codex_vscode/0.164.0 (Windows 11; aarch64) WindowsTerminal", "codex_vscode", "0.164.0"},
		} {
			t.Run(kind+"/"+tc.name, func(t *testing.T) {
				settings := &SettingService{settingRepo: &openAIIdentitySettingRepoStub{values: map[string]string{
					SettingKeyOpenAICodexUserAgent: tc.globalUA, SettingKeyOpenAICodexClientVersion: tc.pinnedVersion,
				}}}
				account := &Account{ID: 71, Platform: PlatformOpenAI, Type: kind, Credentials: map[string]any{
					"user_agent": tc.accountUA, "api_key": "fixture", "access_token": "fixture", "chatgpt_account_id": "owner-71",
				}}
				identity := resolveOpenAIOutboundIdentityFromSettings(context.Background(), account, settings)
				require.Equal(t, tc.source, identity.Source)
				svc := &OpenAIGatewayService{cfg: codexTransportTestConfig(), settingService: settings}
				for _, passthrough := range []bool{false, true} {
					body := []byte(`{"model":"gpt-6.1-sol","stream":true,"input":"hello"}`)
					c := newOpenAIRejectedFieldTestContext(body)
					c.Request.Header.Set("User-Agent", "codex-tui/9.9.9 (Untrusted; unknown) caller")
					c.Request.Header.Set("Originator", "codex-tui")
					c.Request.Header.Set("Version", "9.9.9")
					for _, h := range captureCodexProtocolHeaders(t, svc, c, account, body, passthrough) {
						require.Equal(t, tc.ua, h.Get("User-Agent"))
						require.Equal(t, tc.originator, h.Get("Originator"))
						require.Equal(t, tc.version, h.Get("Version"))
					}
				}
			})
		}
	}
}
