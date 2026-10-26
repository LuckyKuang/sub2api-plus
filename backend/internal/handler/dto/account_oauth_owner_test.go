//go:build unit

package dto

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/service"
	"github.com/stretchr/testify/require"
)

// Admin responses intentionally redact tokens. A UI must nevertheless observe
// replacement of its initiating login without recovering or displaying secrets.
func TestAccountDTOExposesOpaqueOAuthOwnerWithoutLeakingCredentials(t *testing.T) {
	for _, platform := range []string{"openai", "grok", "deepseek", "kimi", "minimax", "stepfun", "zhipu"} {
		t.Run(platform, func(t *testing.T) {
			proxyID := int64(31)
			account := &service.Account{ID: 81, Platform: platform, Type: service.AccountTypeOAuth, ProxyID: &proxyID,
				Credentials: map[string]any{"access_token": "private-original-access", "refresh_token": "private-refresh"},
				Proxy:       &service.Proxy{ID: proxyID, Protocol: "http", Host: "proxy.example", Port: 8080, Username: "private-proxy-user", Password: "private-proxy-password", Status: service.StatusActive},
			}
			observe := func() string {
				wire, err := json.Marshal(AccountFromServiceShallow(account))
				require.NoError(t, err)
				for _, secret := range []string{"private-original-access", "private-replacement-access", "private-refresh", "private-proxy-user", "private-proxy-password"} {
					require.NotContains(t, string(wire), secret)
				}
				var dto map[string]any
				require.NoError(t, json.Unmarshal(wire, &dto))
				version, ok := dto["oauth_credential_owner"].(string)
				require.True(t, ok, "redaction cannot hide an owner replacement from an active reauthorization flow")
				require.Regexp(t, `^[a-f0-9]{64}$`, version)
				return version
			}
			original := observe()
			account.Extra = map[string]any{"usage_percent": 42}
			account.Name = "renamed account"
			account.UpdatedAt = time.Now()
			require.Equal(t, original, observe(), "unrelated management or usage updates cannot revoke authorization")
			account.Credentials["access_token"] = "private-replacement-access"
			replacement := observe()
			require.NotEqual(t, original, replacement)
			account.Proxy.Port++
			require.NotEqual(t, replacement, observe(), "same-row route edits must be observable even when proxy_id stays the same")
		})
	}
}
