//go:build unit || !integration

package service

import (
	"context"
	"net/http"
	"testing"

	"github.com/LuckyKuang/sub2api-plus/internal/pkg/outboundidentity"
	"github.com/stretchr/testify/require"
)

func TestUbuntuOutboundDefaultEnvironmentAndSourcePriority(t *testing.T) {
	defaults := emptyOutboundIdentitySettings()
	defaults.Runtime["kimi"] = map[string]string{"X-Msh-Device-Name": "custom-workstation"}
	svc, ctx := outboundIdentityTestSettings(t, defaults)
	require.Equal(t, "ubuntu", builtInOutboundIdentity("kimi").Headers["X-Msh-Device-Name"])
	require.Equal(t, "Linux 6.8.0-31-generic x64", builtInOutboundIdentity("kimi").Headers["X-Msh-Device-Model"])
	require.Equal(t, "custom-workstation", svc.resolveDefaultOutboundIdentity(ctx, "kimi").Headers["X-Msh-Device-Name"])
	for _, accountType := range []string{AccountTypeOAuth, AccountTypeAPIKey} {
		account := &Account{ID: 123, Platform: PlatformKimi, Type: accountType, Credentials: map[string]any{outboundIdentityCredential: OutboundIdentitySelection{Preset: "kimi", Headers: map[string]string{"X-Msh-Device-Name": "account-workstation"}}}}
		identity, ok := outboundidentity.FromContext(WithAccountOutboundIdentity(ctx, account))
		require.True(t, ok)
		require.Equal(t, "account", identity.Source)
		require.Equal(t, "account-workstation", identity.Headers["X-Msh-Device-Name"])
		require.Equal(t, "6.8.0-31-generic", identity.Headers["X-Msh-Os-Version"])
	}
	for _, preset := range []string{"claude", "minimax", "minimax_apikey", "kimi"} {
		identity := builtInOutboundIdentity(preset).ForProtocol("anthropic")
		require.Equal(t, "Linux", identity.Headers["X-Stainless-OS"], preset)
		require.Equal(t, "x64", identity.Headers["X-Stainless-Arch"], preset)
	}
	require.Equal(t, "GeminiCLI/0.1.5 (Linux; x64)", builtInOutboundIdentity("gemini").UserAgent)
	require.Equal(t, "linux-x64", builtInOutboundIdentity("zcode").Headers["X-Platform"])
	require.Equal(t, "6.8.0-31-generic", builtInOutboundIdentity("zcode").Headers["X-Os-Version"])
	require.Equal(t, DefaultOpenAICodexUserAgent, builtInOutboundIdentity("codex").UserAgent)
}

func TestOutboundEnvironmentValidationIsAtomic(t *testing.T) {
	svc, ctx := outboundIdentityTestSettings(t, emptyOutboundIdentitySettings())
	for _, bad := range []OutboundIdentitySelection{
		{Preset: "kimi", Headers: map[string]string{"X-Msh-Device-Name": "sub2api-apple"}},
		{Preset: "zcode", Headers: map[string]string{"X-Client-Language": "en_US"}},
		{Preset: "zcode", Headers: map[string]string{"X-Client-Timezone": "Local"}},
		{Preset: "minimax", Timezone: "invalid"},
		{Preset: "kimi", Timezone: "UTC"},
		{Preset: "deepseek", Language: "zh_CN"},
		{Preset: "deepseek", Language: "ja-JP"},
		{Preset: "deepseek", Timezone: "Local"},
		{Preset: "kimi", Language: "en-US"},
		{Preset: "kimi", Headers: map[string]string{"X-Msh-Device-Model": "Linux 6.8.0-31-generic x64"}},
		{Preset: "kimi", Headers: map[string]string{"X-Msh-Os-Version": "6.8.0-31-generic"}},
		{Preset: "zcode", Headers: map[string]string{"X-Platform": "linux-x64"}},
		{Preset: "zcode", Headers: map[string]string{"X-Os-Category": "linux"}},
		{Preset: "zcode", Headers: map[string]string{"X-Os-Version": "6.8.0-31-generic"}},
	} {
		config := emptyOutboundIdentitySettings()
		config.Profiles[bad.Preset] = bad
		require.Error(t, svc.SetOutboundIdentitySettings(ctx, config))
		require.Error(t, NormalizeAccountOutboundIdentity(bad.Preset, AccountTypeAPIKey, map[string]any{outboundIdentityCredential: bad}))
	}
}

func TestMiniMaxTimezoneSnapshotInheritanceRetryAndFailover(t *testing.T) {
	for _, accountType := range []string{AccountTypeOAuth, AccountTypeAPIKey} {
		preset := nativeAccountOutboundPreset(PlatformMiniMax, accountType)
		defaults := emptyOutboundIdentitySettings()
		defaults.Profiles[preset] = OutboundIdentitySelection{Preset: preset, Timezone: "Asia/Shanghai"}
		_, ctx := outboundIdentityTestSettings(t, defaults)
		ctx = WithOutboundIdentityScope(ctx, nil)
		a := &Account{ID: 1, Platform: PlatformMiniMax, Type: accountType, Credentials: map[string]any{outboundIdentityCredential: OutboundIdentitySelection{Preset: preset}}}
		request := func(ctx context.Context, owner *Account) *http.Request {
			req, err := http.NewRequestWithContext(ctx, "POST", "https://api.minimaxi.com/anthropic/v1/messages", nil)
			require.NoError(t, err)
			req.Header.Set("X-Mavis-Timezone-Offset", "999")
			return prepareAccountOutboundRequest(req, owner)
		}
		first := request(ctx, a)
		require.Equal(t, "28800", first.Header.Get("X-Mavis-Timezone-Offset"))
		captured, _ := outboundidentity.FromContext(first.Context())
		require.Equal(t, "Asia/Shanghai", selectionFromIdentity(captured).Timezone)
		a.Credentials[outboundIdentityCredential] = OutboundIdentitySelection{Preset: preset, Timezone: "UTC"}
		retry := request(first.Context(), a)
		require.Equal(t, first.Header.Get("X-Mavis-Timezone-Offset"), retry.Header.Get("X-Mavis-Timezone-Offset"))
		require.Equal(t, first.Header.Get("X-Mavis-Session-Id"), retry.Header.Get("X-Mavis-Session-Id"))
		other := *a
		other.ID = 2
		failover := request(first.Context(), &other)
		require.Equal(t, "0", failover.Header.Get("X-Mavis-Timezone-Offset"))
		require.NotEqual(t, first.Header.Get("X-Mavis-Session-Id"), failover.Header.Get("X-Mavis-Session-Id"))
	}
}

func TestMiniMaxInvalidTimezoneFallsThroughAtomically(t *testing.T) {
	config := emptyOutboundIdentitySettings()
	config.Defaults["minimax:apikey"] = "codex"
	_, ctx := outboundIdentityTestSettings(t, config)
	account := &Account{ID: 88, Platform: PlatformMiniMax, Type: AccountTypeAPIKey, Credentials: map[string]any{outboundIdentityCredential: OutboundIdentitySelection{Preset: "minimax_apikey", Timezone: "Local"}}}
	identity, ok := outboundidentity.FromContext(WithAccountOutboundIdentity(ctx, account))
	require.True(t, ok)
	require.Equal(t, "codex", identity.Preset)
	require.Empty(t, identity.Timezone)
}

func TestDeepSeekLanguageTimezoneSourcePriorityAndPreview(t *testing.T) {
	for _, accountType := range []string{AccountTypeOAuth, AccountTypeAPIKey} {
		defaults := emptyOutboundIdentitySettings()
		defaults.Profiles["deepseek"] = OutboundIdentitySelection{Preset: "deepseek", Language: "en-US", Timezone: "Asia/Shanghai"}
		svc, ctx := outboundIdentityTestSettings(t, defaults)
		global := svc.resolveDefaultOutboundIdentity(ctx, "deepseek")
		require.Equal(t, "en-US", global.Language)
		require.Equal(t, "Asia/Shanghai", global.Timezone)
		for _, selection := range []OutboundIdentitySelection{
			{Preset: "deepseek"},
			{Preset: "deepseek", Version: "0.2.0-rc.2"},
			{Preset: "deepseek", Language: "zh-CN", Timezone: "UTC"},
			{Preset: "deepseek", Language: "ja-JP", Timezone: "UTC"},
		} {
			account := &Account{ID: 71, Platform: PlatformDeepseek, Type: accountType, Credentials: map[string]any{outboundIdentityCredential: selection}}
			identity, ok := outboundidentity.FromContext(WithAccountOutboundIdentity(ctx, account))
			require.True(t, ok)
			if selection.Language == "zh-CN" {
				require.Equal(t, "zh-CN", identity.Language)
				require.Equal(t, "UTC", identity.Timezone)
			} else {
				require.Equal(t, "en-US", identity.Language)
				require.Equal(t, "Asia/Shanghai", identity.Timezone)
			}
			preview, err := svc.PreviewOutboundIdentity(ctx, account, &selection)
			if selection.Language == "ja-JP" {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				require.Equal(t, identity.Language, preview.Language)
				require.Equal(t, identity.Timezone, preview.Timezone)
			}
			pinned := selectionFromIdentity(identity)
			require.Equal(t, identity.Language, pinned.Language)
			require.Equal(t, identity.Timezone, pinned.Timezone)
		}
		view := svc.GetOutboundIdentityView(ctx)
		for _, identity := range view.ControlPlane {
			if identity.Preset == "deepseek" {
				require.Equal(t, "en_US", identity.Headers["X-Client-Locale"])
				require.Equal(t, "28800", identity.Headers["X-Client-Timezone-Offset"])
			}
		}
	}
}
