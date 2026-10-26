//go:build unit

package admin

import (
	"context"
	"testing"

	"github.com/LuckyKuang/sub2api-plus/internal/pkg/oauth"
	"github.com/LuckyKuang/sub2api-plus/internal/service"
	"github.com/stretchr/testify/require"
)

type forbiddenForeignRefreshClient struct {
	service.ClaudeOAuthClient
	calls int
}

func (c *forbiddenForeignRefreshClient) RefreshToken(context.Context, string, string) (*oauth.TokenResponse, error) {
	c.calls++
	return &oauth.TokenResponse{AccessToken: "foreign-provider-token", RefreshToken: "foreign-provider-refresh", ExpiresIn: 3600}, nil
}

// DeepSeek publishes no refresh grant. ZCode's short project PAT lifecycle is
// independent of business-login refresh. Neither account may send its token
// to the default Claude issuer, including imported incomplete native records.
func TestNativeAccountsWithoutBusinessRefreshNeverUseForeignIssuer(t *testing.T) {
	for _, platform := range []string{service.PlatformDeepseek, service.PlatformZhipu} {
		for _, marker := range []string{"native-marker", "missing-native-marker"} {
			t.Run(platform+"/"+marker, func(t *testing.T) {
				account := &service.Account{ID: 81, Platform: platform, Type: service.AccountTypeOAuth, Credentials: map[string]any{
					"access_token": "private-native-access", "refresh_token": "private-native-refresh",
				}}
				if marker == "native-marker" {
					account.Credentials["oauth_provider"] = platform
				}
				foreign := &forbiddenForeignRefreshClient{}
				admin := newStubAdminService()
				handler := NewAccountHandler(admin, service.NewOAuthService(nil, foreign), nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
				updated, _, err := handler.refreshSingleAccount(context.Background(), account)
				require.Zero(t, foreign.calls, "an unsupported grant cannot disclose its refresh token to another platform")
				require.Error(t, err)
				require.Nil(t, updated)
				require.Zero(t, admin.updateAccountCalls)
				require.Zero(t, admin.updateAccountExtraCalls)
			})
		}
	}
}
