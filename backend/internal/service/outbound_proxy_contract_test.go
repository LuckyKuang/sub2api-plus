//go:build unit || !integration

package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

// Binding is authoritative. Malformed or mismatched loaded proxy data must not
// turn a configured proxy into an empty/direct transport parameter.
func TestRequestCredentialProxyContractSevenPlatforms(t *testing.T) {
	for _, platform := range []string{PlatformOpenAI, PlatformGrok, PlatformDeepseek, PlatformKimi, PlatformMiniMax, PlatformStepFun, PlatformZhipu} {
		for _, kind := range []string{AccountTypeOAuth, AccountTypeAPIKey} {
			t.Run(platform+"/"+kind, func(t *testing.T) {
				credentials := func() map[string]any {
					values := map[string]any{"access_token": "fixture-token", "api_key": "fixture-token"}
					if platform == PlatformZhipu && kind == AccountTypeOAuth {
						values["oauth_provider"] = "bigmodel"
					}
					return values
				}
				for _, failure := range []string{"missing", "mismatched", "zero-id", "bad-protocol", "empty-host", "zero-port", "large-port"} {
					t.Run(failure, func(t *testing.T) {
						id := int64(19)
						account := &Account{ID: 71, Platform: platform, Type: kind, Credentials: credentials(), ProxyID: &id, Proxy: &Proxy{ID: id, Protocol: "http", Host: "bound.invalid", Port: 8080, Username: "private-user", Password: "private-password"}}
						switch failure {
						case "missing":
							account.Proxy = nil
						case "mismatched":
							account.Proxy.ID = 20
						case "zero-id":
							id = 0
							account.Proxy.ID = 0
						case "bad-protocol":
							account.Proxy.Protocol = "direct"
						case "empty-host":
							account.Proxy.Host = ""
						case "zero-port":
							account.Proxy.Port = 0
						case "large-port":
							account.Proxy.Port = 65536
						}
						for _, captured := range []bool{false, true} {
							ctx := context.Background()
							if captured {
								ctx = withRequestCredentialOwner(ctx, account, "fixture-token")
							}
							route, err := requestCredentialOwnerProxyURL(ctx, account)
							require.Error(t, err)
							require.Empty(t, route)
							require.NotContains(t, err.Error(), "private-")
						}
					})
				}
				id := int64(19)
				account := &Account{ID: 71, Platform: platform, Type: kind, Credentials: credentials(), ProxyID: &id, Proxy: &Proxy{ID: id, Protocol: "http", Host: "original.invalid", Port: 8080}}
				ctx := withRequestCredentialOwner(context.Background(), account, "fixture-token")
				account.Proxy = &Proxy{ID: 20, Protocol: "http", Host: "replacement.invalid", Port: 9080}
				account.ProxyID = &account.Proxy.ID
				captured, err := requestCredentialOwnerProxyURL(ctx, account)
				require.NoError(t, err)
				require.Equal(t, "http://original.invalid:8080", captured, "an acquired attempt keeps its complete original route")
				current, err := requestCredentialOwnerProxyURL(context.Background(), account)
				require.NoError(t, err)
				require.Equal(t, "http://replacement.invalid:9080", current, "new operations use the committed route")
				account.ProxyID = nil
				direct, err := requestCredentialOwnerProxyURL(context.Background(), account)
				require.NoError(t, err)
				require.Empty(t, direct, "an explicitly unbound account ignores stale hydrated proxy data")
			})
		}
	}
}
