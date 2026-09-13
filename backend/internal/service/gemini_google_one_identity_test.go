//go:build unit

package service

import (
	"context"
	"testing"

	"github.com/LuckyKuang/sub2api-plus/internal/pkg/geminicli"
	"github.com/LuckyKuang/sub2api-plus/internal/pkg/outboundidentity"
	"github.com/stretchr/testify/require"
)

func TestGoogleOneTierRefreshUsesCredentialOwnerIdentity(t *testing.T) {
	config := emptyOutboundIdentitySettings()
	config.Profiles["gemini"] = OutboundIdentitySelection{Preset: "gemini", Version: "3.9.1"}
	_, ctx := outboundIdentityTestSettings(t, config)
	foreign := &Account{ID: 5, Platform: PlatformAnthropic, Type: AccountTypeOAuth}
	ctx = WithAccountOutboundIdentity(ctx, foreign)
	var captured outboundidentity.Identity
	svc := &GeminiOAuthService{driveClient: &mockDriveClient{getStorageQuotaFunc: func(ctx context.Context, accessToken, proxyURL string) (*geminicli.DriveStorageInfo, error) {
		require.Equal(t, "google-one-token", accessToken)
		var ok bool
		captured, ok = outboundidentity.FromContext(ctx)
		require.True(t, ok)
		return &geminicli.DriveStorageInfo{Limit: 100 * 1024 * 1024 * 1024}, nil
	}}}
	account := &Account{ID: 6, Platform: PlatformGemini, Type: AccountTypeOAuth, Credentials: map[string]any{"oauth_type": "google_one", "access_token": "google-one-token"}}
	_, _, _, err := svc.RefreshAccountGoogleOneTier(ctx, account)
	require.NoError(t, err)
	require.Equal(t, account.ID, captured.AccountID)
	require.Equal(t, "3.9.1", captured.Version)
	account.Credentials[outboundIdentityCredential] = OutboundIdentitySelection{Preset: "gemini", Version: "3.9.2"}
	_, _, _, err = svc.RefreshAccountGoogleOneTier(ctx, account)
	require.NoError(t, err)
	require.Equal(t, "3.9.1", captured.Version, "same operation retains its first identity")
	_, fresh := outboundIdentityTestSettings(t, config)
	_, _, _, err = svc.RefreshAccountGoogleOneTier(fresh, account)
	require.NoError(t, err)
	require.Equal(t, "3.9.2", captured.Version)
	require.Equal(t, "account", captured.Source)
}
