//go:build unit || !integration

package service

import (
	"net/http"
	"testing"

	"github.com/LuckyKuang/sub2api-plus/internal/pkg/claude"
	"github.com/LuckyKuang/sub2api-plus/internal/pkg/outboundidentity"
	"github.com/stretchr/testify/require"
)

func TestClaudeIdentityDefaultUpgradePreservesExplicitPins(t *testing.T) {
	t.Setenv(claude.CLIVersionEnv, "")
	for _, accountType := range []string{AccountTypeOAuth, AccountTypeAPIKey, AccountTypeSetupToken, AccountTypeBedrock, AccountTypeServiceAccount} {
		t.Run(accountType, func(t *testing.T) {
			config := emptyOutboundIdentitySettings()
			config.Profiles["claude"] = OutboundIdentitySelection{Preset: "claude", Version: "2.1.300"}
			_, ctx := outboundIdentityTestSettings(t, config)
			account := &Account{ID: 42, Platform: PlatformAnthropic, Type: accountType, Credentials: map[string]any{}}
			for _, pin := range []struct{ source, version string }{{"global", "2.1.300"}, {"account", "2.1.258"}, {"account", "2.1.320"}} {
				if pin.source == "account" {
					account.Credentials[outboundIdentityCredential] = OutboundIdentitySelection{Preset: "claude", Version: pin.version}
				}
				snapshot := WithAccountOutboundIdentity(ctx, account)
				identity, ok := outboundidentity.FromContext(snapshot)
				require.True(t, ok)
				require.Equal(t, pin.source, identity.Source)
				require.Equal(t, pin.version, identity.Version)
				require.Equal(t, "claude-cli/"+pin.version+" (external, cli)", identity.UserAgent)
				req, err := http.NewRequestWithContext(snapshot, http.MethodPost, "https://api.anthropic.com/v1/messages", nil)
				require.NoError(t, err)
				prepareAccountOutboundRequest(req, account)
				require.Equal(t, identity.UserAgent, req.UserAgent())
				require.Equal(t, "0.94.0", req.Header.Get("X-Stainless-Package-Version"))
				require.Equal(t, "v24.3.0", req.Header.Get("X-Stainless-Runtime-Version"))
				require.Equal(t, "cli", req.Header.Get("X-App"))
			}
			account.Credentials[outboundIdentityCredential] = OutboundIdentitySelection{Preset: "claude", Version: "2.1.257"}
			identity, _ := outboundidentity.FromContext(WithAccountOutboundIdentity(ctx, account))
			require.Equal(t, "global", identity.Source)
			require.Equal(t, "2.1.300", identity.Version)
		})
	}
}
