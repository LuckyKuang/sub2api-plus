//go:build unit

package service

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/config"
	"github.com/LuckyKuang/sub2api-plus/internal/pkg/antigravity"
	"github.com/LuckyKuang/sub2api-plus/internal/pkg/geminicli"
	"github.com/LuckyKuang/sub2api-plus/internal/pkg/outboundidentity"
	"github.com/stretchr/testify/require"
)

func capturePrivacyIdentity(t *testing.T, ctx context.Context) []*http.Request {
	t.Helper()
	previous := http.DefaultTransport
	defer func() { http.DefaultTransport = previous }()
	var captured []*http.Request
	http.DefaultTransport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		captured = append(captured, req.Clone(req.Context()))
		return newJSONResponse(http.StatusOK, `{}`), nil
	})
	require.Equal(t, AntigravityPrivacySet, setAntigravityPrivacy(ctx, "test-token", "test-project", ""))
	require.Len(t, captured, 2)
	return captured
}

func TestAntigravityPrivacyIdentitySourcePriority(t *testing.T) {
	for _, source := range []string{"account", "global", "invalid_account", "invalid_global", "environment", "compiled_default"} {
		t.Run(source, func(t *testing.T) {
			t.Setenv(antigravity.AntigravityUserAgentVersionEnv, "")
			settings := emptyOutboundIdentitySettings()
			account := &Account{ID: 72, Platform: PlatformAntigravity, Type: AccountTypeOAuth, Credentials: map[string]any{}}
			expectedSource, expectedVersion := source, antigravity.DefaultUserAgentVersion
			switch source {
			case "account", "global", "invalid_account":
				t.Setenv(antigravity.AntigravityUserAgentVersionEnv, "1.20.8")
				settings.Profiles["antigravity"] = OutboundIdentitySelection{Preset: "antigravity", Version: "1.20.5"}
				expectedSource, expectedVersion = "global", "1.20.5"
				if source == "account" {
					account.Credentials[outboundIdentityCredential] = OutboundIdentitySelection{Preset: "antigravity", UserAgent: "antigravity/1.20.6 linux/arm64", Version: "1.20.9"}
					expectedSource, expectedVersion = "account", "1.20.9"
				} else if source == "invalid_account" {
					account.Credentials[outboundIdentityCredential] = OutboundIdentitySelection{Preset: "antigravity", UserAgent: "untrusted"}
				}
			case "environment", "invalid_global":
				t.Setenv(antigravity.AntigravityUserAgentVersionEnv, "1.20.8")
				expectedSource, expectedVersion = "environment", "1.20.8"
			}
			svc, ctx := outboundIdentityTestSettings(t, settings)
			if source == "invalid_global" {
				settings.Profiles["antigravity"] = OutboundIdentitySelection{Preset: "antigravity", UserAgent: "untrusted"}
				svc.outboundIdentityCache.Store(&cachedOutboundIdentitySettings{settings: settings, expires: time.Now().Add(time.Minute)})
			}
			ctx = WithAccountOutboundIdentity(ctx, account)
			base, ok := outboundidentity.FromContext(ctx)
			require.True(t, ok)
			require.Equal(t, expectedSource, base.Source)
			require.Equal(t, expectedVersion, base.Version)
			if source == "account" {
				require.Equal(t, "antigravity/1.20.9 linux/arm64", base.UserAgent, "version-only changes preserve the system fingerprint")
			}
			for _, req := range capturePrivacyIdentity(t, ctx) {
				require.Equal(t, base.UserAgent, req.UserAgent())
				require.Equal(t, "gl-node/22.21.1", req.Header.Get("X-Goog-Api-Client"))
				require.Empty(t, req.Header.Get("Originator"))
				require.Empty(t, req.Header.Get("Version"))
				snapshot, ok := outboundidentity.FromContext(req.Context())
				require.True(t, ok)
				require.Equal(t, base.AccountID, snapshot.AccountID)
				require.Equal(t, base.Source, snapshot.Source)
				require.Equal(t, base.Version, snapshot.Version)
			}
			parent, _ := outboundidentity.FromContext(ctx)
			require.Equal(t, base, parent)
		})
	}
}

func TestAntigravityOAuthIdentityAcrossDiscoveryAndPrivacy(t *testing.T) {
	for _, entry := range []string{"exchange", "validate_refresh"} {
		t.Run(entry, func(t *testing.T) {
			settings := emptyOutboundIdentitySettings()
			settings.Profiles["antigravity"] = OutboundIdentitySelection{Preset: "antigravity", Version: "1.20.5"}
			settings.Defaults["antigravity:apikey"] = "claude"
			svc, ctx := outboundIdentityTestSettings(t, settings)
			ctx = WithAccountOutboundIdentity(WithOutboundIdentityScope(ctx, nil), &Account{ID: 99, Platform: PlatformOpenAI, Type: AccountTypeOAuth})
			oauth := NewAntigravityOAuthService(nil)
			t.Cleanup(oauth.Stop)
			previous := http.DefaultTransport
			defer func() { http.DefaultTransport = previous }()
			for operation, version := range []string{"1.20.5", "1.20.9"} {
				var paths []string
				http.DefaultTransport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
					paths = append(paths, req.URL.Path)
					require.Equal(t, "antigravity/"+version+" windows/amd64", req.UserAgent())
					identity, ok := outboundidentity.FromContext(req.Context())
					require.True(t, ok)
					require.Zero(t, identity.AccountID)
					require.Equal(t, "global", identity.Source)
					require.Equal(t, version, identity.Version)
					require.Empty(t, req.Header.Get("Originator"))
					body := `{}`
					switch req.URL.Path {
					case "/token":
						body = `{"access_token":"test-token","refresh_token":"test-refresh","expires_in":3600,"token_type":"Bearer"}`
					case "/oauth2/v2/userinfo":
						body = `{"email":"test@example.com"}`
					case "/v1internal:loadCodeAssist":
						payload, err := io.ReadAll(req.Body)
						require.NoError(t, err)
						var load antigravity.LoadCodeAssistRequest
						require.NoError(t, json.Unmarshal(payload, &load))
						require.Equal(t, version, load.Metadata.IDEVersion)
						body = `{"cloudaicompanionProject":"test-project"}`
					case "/v1internal:setUserSettings", "/v1internal:fetchUserInfo":
						require.Equal(t, "gl-node/22.21.1", req.Header.Get("X-Goog-Api-Client"))
					default:
						t.Fatalf("unexpected provider request: %s", req.URL.Path)
					}
					if !strings.Contains(req.URL.Path, "User") {
						require.Empty(t, req.Header.Get("X-Goog-Api-Client"))
					}
					if operation == 0 {
						settings.Profiles["antigravity"] = OutboundIdentitySelection{Preset: "antigravity", Version: "1.20.9"}
						require.NoError(t, svc.SetOutboundIdentitySettings(ctx, settings))
					}
					before := req.Header.Clone()
					outboundidentity.ApplyContext(req)
					require.Equal(t, before, req.Header)
					return newJSONResponse(http.StatusOK, body), nil
				})
				var result *AntigravityTokenInfo
				var err error
				if entry == "exchange" {
					oauth.sessionStore.Set("test-session", &antigravity.OAuthSession{State: "test-state", CodeVerifier: "test-verifier", CreatedAt: time.Now()})
					result, err = oauth.ExchangeCode(ctx, &AntigravityExchangeCodeInput{SessionID: "test-session", State: "test-state", Code: "test-code"})
				} else {
					result, err = oauth.ValidateRefreshToken(ctx, "test-refresh", nil)
				}
				require.NoError(t, err)
				require.Equal(t, AntigravityPrivacySet, result.PrivacyMode)
				require.Equal(t, []string{"/token", "/oauth2/v2/userinfo", "/v1internal:loadCodeAssist", "/v1internal:setUserSettings", "/v1internal:fetchUserInfo"}, paths)
			}
		})
	}
}

func TestGeminiOAuthIdentitySnapshotThroughDrive(t *testing.T) {
	settings := emptyOutboundIdentitySettings()
	settings.Profiles["gemini"] = OutboundIdentitySelection{Preset: "gemini", Version: "3.9.1"}
	settings.Defaults["gemini:apikey"] = "claude"
	svc, ctx := outboundIdentityTestSettings(t, settings)
	ctx = WithAccountOutboundIdentity(WithOutboundIdentityScope(ctx, nil), &Account{ID: 99, Platform: PlatformAnthropic, Type: AccountTypeOAuth})
	var captured []outboundidentity.Identity
	capture := func(ctx context.Context) {
		identity, ok := outboundidentity.FromContext(ctx)
		require.True(t, ok)
		captured = append(captured, identity)
	}
	oauth := NewGeminiOAuthService(nil, &mockGeminiOAuthClient{exchangeCodeFunc: func(ctx context.Context, _, _, _, _, _ string) (*geminicli.TokenResponse, error) {
		capture(ctx)
		settings.Profiles["gemini"] = OutboundIdentitySelection{Preset: "gemini", Version: "3.9.2"}
		require.NoError(t, svc.SetOutboundIdentitySettings(ctx, settings))
		return &geminicli.TokenResponse{AccessToken: "test-token", ExpiresIn: 3600}, nil
	}}, nil, &mockDriveClient{getStorageQuotaFunc: func(ctx context.Context, token, _ string) (*geminicli.DriveStorageInfo, error) {
		require.Equal(t, "test-token", token)
		capture(ctx)
		return &geminicli.DriveStorageInfo{Limit: StorageTierBasic}, nil
	}}, &config.Config{})
	t.Cleanup(oauth.Stop)
	for _, version := range []string{"3.9.1", "3.9.2"} {
		captured = nil
		oauth.sessionStore.Set("test-session", &geminicli.OAuthSession{State: "test-state", CreatedAt: time.Now(), OAuthType: "google_one", ProjectID: "test-project"})
		result, err := oauth.ExchangeCode(ctx, &GeminiExchangeCodeInput{SessionID: "test-session", State: "test-state"})
		require.NoError(t, err)
		require.Equal(t, "google_one", result.OAuthType)
		require.Len(t, captured, 2)
		require.Equal(t, captured[0], captured[1])
		require.Equal(t, "gemini", captured[0].Preset)
		require.Equal(t, version, captured[0].Version)
		require.Equal(t, "global", captured[0].Source)
		require.Zero(t, captured[0].AccountID)
		require.NotContains(t, captured[0].Headers, "X-Goog-Api-Client")
	}
}

func TestNativeOAuthIdentityWithoutResolver(t *testing.T) {
	ctx := outboundidentity.WithResolver(context.Background(), func(context.Context, string) outboundidentity.Identity { return outboundidentity.Identity{} })
	for _, platform := range []string{PlatformGemini, PlatformAntigravity} {
		identity, ok := outboundidentity.FromContext(withNativeOAuthOutboundIdentity(ctx, platform))
		require.True(t, ok)
		require.Equal(t, builtInOutboundIdentity(nativeOutboundPreset(platform)), identity)
	}
}
