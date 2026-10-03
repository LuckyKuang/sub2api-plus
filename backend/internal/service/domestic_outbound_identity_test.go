//go:build unit || !integration

package service

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/LuckyKuang/sub2api-plus/internal/pkg/brandidentity"
	"github.com/LuckyKuang/sub2api-plus/internal/pkg/outboundidentity"
	"github.com/LuckyKuang/sub2api-plus/internal/pkg/zcode"
	"github.com/stretchr/testify/require"
)

func TestDomesticOutboundIdentityWireSnapshotAndFailover(t *testing.T) {
	for _, platform := range []string{PlatformDeepseek, PlatformKimi, PlatformMiniMax, PlatformZhipu} {
		for _, accountType := range []string{AccountTypeOAuth, AccountTypeAPIKey} {
			t.Run(platform+"/"+accountType, func(t *testing.T) {
				preset := nativeOutboundPreset(platform)
				config := emptyOutboundIdentitySettings()
				selection := OutboundIdentitySelection{Preset: preset}
				if preset != "minimax" {
					selection.Version = "4.1.0"
				}
				config.Profiles[preset] = selection
				svc, ctx := outboundIdentityTestSettings(t, config)
				account := &Account{ID: 101, Platform: platform, Type: accountType}
				ctx = WithAccountOutboundIdentity(ctx, account)
				pinned, ok := outboundidentity.FromContext(ctx)
				require.True(t, ok)
				preview, err := svc.PreviewOutboundIdentity(ctx, account, nil)
				require.NoError(t, err)
				require.Equal(t, pinned.Headers, preview.Headers)
				captured := make(chan http.Header, 1)
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					captured <- r.Header.Clone()
					w.WriteHeader(http.StatusNoContent)
				}))
				defer server.Close()
				client := &http.Client{Transport: brandidentity.WrapRoundTripper(server.Client().Transport)}
				// A setting change between attempts must not mutate the owner snapshot.
				if preset != "minimax" {
					selection.Version = "4.2.0"
				}
				config.Profiles[preset] = selection
				require.NoError(t, svc.SetOutboundIdentitySettings(ctx, config))
				for _, path := range []string{"/v1/chat/completions", "/v1/messages", "/v1/models", "/usage"} {
					req, err := http.NewRequestWithContext(ctx, http.MethodPost, server.URL+path, nil)
					require.NoError(t, err)
					// Request construction replaces caller and SDK declarations.
					req.Header.Set("User-Agent", "foreign-sdk/99.0.0")
					req.Header.Set("X-ZCode-App-Version", "99.0.0")
					req.Header.Set("X-Msh-Platform", "foreign")
					req.Header.Set("X-Title", "foreign")
					req.Header.Set("Authorization", "Bearer test-owner-token")
					req.Header.Set("Anthropic-Version", "2023-06-01")
					prepareAccountOutboundRequest(req, account)
					resp, err := client.Do(req)
					require.NoError(t, err)
					require.NoError(t, resp.Body.Close())
					got := <-captured
					for name, value := range pinned.Headers {
						require.Equal(t, value, got.Get(name), name)
					}
					require.Empty(t, got.Get("Originator"))
					require.Empty(t, got.Get("Version"))
					if preset != "zcode" {
						require.Empty(t, got.Get(zcode.HeaderAppVersion))
						require.Empty(t, got.Get("X-Title"))
					}
					if preset != "kimi" {
						require.Empty(t, got.Get("X-Msh-Platform"))
					}
					require.Equal(t, "Bearer test-owner-token", got.Get("Authorization"))
					require.Equal(t, "2023-06-01", got.Get("Anthropic-Version"))
				}
				next := &Account{ID: 102, Platform: platform, Type: accountType}
				switched, ok := outboundidentity.FromContext(WithAccountOutboundIdentity(ctx, next))
				require.True(t, ok)
				require.EqualValues(t, 102, switched.AccountID)
				if preset != "minimax" {
					require.Equal(t, "4.2.0", switched.Version)
					require.Equal(t, "4.1.0", pinned.Version)
				}
				// Native OAuth bootstrap ignores a compatible API-key type mapping.
				config.Defaults[platform+":apikey"] = "codex"
				require.NoError(t, svc.SetOutboundIdentitySettings(ctx, config))
				bootstrap, ok := outboundidentity.FromContext(withNativeOAuthOutboundIdentity(ctx, platform))
				require.True(t, ok)
				require.Equal(t, preset, bootstrap.Preset)
				require.Zero(t, bootstrap.AccountID)
			})
		}
	}
}

func TestDomesticIdentityInvalidHeaderCandidateFallsThroughAtomically(t *testing.T) {
	for _, preset := range []string{"deepseek", "kimi", "minimax", "zcode"} {
		t.Run(preset, func(t *testing.T) {
			platform := preset
			if preset == "zcode" {
				platform = PlatformZhipu
			}
			config := emptyOutboundIdentitySettings()
			global := OutboundIdentitySelection{Preset: preset}
			if preset != "minimax" {
				global.Version = "4.1.0"
			}
			config.Profiles[preset] = global
			svc, ctx := outboundIdentityTestSettings(t, config)
			bad := OutboundIdentitySelection{Preset: preset, Headers: map[string]string{"User-Agent": "injected"}}
			account := &Account{ID: 1, Platform: platform, Type: AccountTypeOAuth, Credentials: map[string]any{outboundIdentityCredential: bad}}
			got, ok := outboundidentity.FromContext(WithAccountOutboundIdentity(ctx, account))
			require.True(t, ok)
			require.Equal(t, "global", got.Source)
			require.Equal(t, svc.resolveDefaultOutboundIdentity(ctx, preset).Headers, got.Headers)
			// Corrupt persisted global profile also falls through as a complete unit.
			config.Profiles[preset] = bad
			raw, err := json.Marshal(config)
			require.NoError(t, err)
			fresh := NewSettingService(&outboundIdentityTestRepo{values: map[string]string{SettingKeyOutboundIdentity: string(raw)}}, nil)
			fallback := fresh.resolveDefaultOutboundIdentity(context.Background(), preset)
			require.Equal(t, builtInOutboundIdentity(preset), fallback)
		})
	}
}

func TestZCodeIdentityRuntimeSettingsAndVersionCompanion(t *testing.T) {
	config := emptyOutboundIdentitySettings()
	config.Runtime["zcode"] = map[string]string{"x-client-timezone": "Europe/Amsterdam"}
	config.Profiles["zcode"] = OutboundIdentitySelection{Preset: "zcode", Version: "4.1.0", Headers: map[string]string{"x-client-language": "zh-CN"}}
	svc, ctx := outboundIdentityTestSettings(t, config)
	view := svc.GetOutboundIdentityView(ctx)
	var headers []OutboundIdentityDeclaration
	for _, item := range view.Declarations {
		if item.Preset == "zcode" {
			headers = item.Headers
		}
	}
	require.Len(t, headers, 11)
	resolved := svc.resolveDefaultOutboundIdentity(ctx, "zcode")
	require.Equal(t, "4.1.0", resolved.Headers[zcode.HeaderAppVersion])
	require.Equal(t, "Europe/Amsterdam", resolved.Headers["X-Client-Timezone"])
	require.Equal(t, "zh-CN", resolved.Headers["X-Client-Language"])
	require.NotContains(t, resolved.Headers, "X-Zcode-App-Version")
	// Config access returns a deep copy, including profile header maps.
	copy := svc.GetOutboundIdentitySettings(ctx)
	copy.Profiles["zcode"].Headers["X-Client-Language"] = "mutated"
	require.Equal(t, "zh-CN", svc.resolveDefaultOutboundIdentity(ctx, "zcode").Headers["X-Client-Language"])
	selection := OutboundIdentitySelection{Preset: "zcode", Version: "4.2.0", Headers: map[string]string{"X-Client-Timezone": "Asia/Shanghai"}}
	account := &Account{ID: 55, Platform: PlatformZhipu, Type: AccountTypeOAuth, Credentials: map[string]any{outboundIdentityCredential: selection}}
	got, ok := outboundidentity.FromContext(WithAccountOutboundIdentity(ctx, account))
	require.True(t, ok)
	require.Equal(t, "4.2.0", got.Headers[zcode.HeaderAppVersion])
	require.Equal(t, "Asia/Shanghai", got.Headers["X-Client-Timezone"])
	require.Equal(t, "zh-CN", got.Headers["X-Client-Language"])
	for _, values := range []map[string]string{
		{"X-ZCode-App-Version": "5.0.0"}, {"X-Title": "foreign"}, {"X-Client-Timezone": "bad\r\nvalue"},
		{"X-Client-Timezone": "UTC", "x-client-timezone": "Asia/Shanghai"},
	} {
		_, err := normalizeOutboundHeaderValues("zcode", values)
		require.Error(t, err)
	}
}
