package service

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/LuckyKuang/sub2api-plus/internal/pkg/brandidentity"
	"github.com/LuckyKuang/sub2api-plus/internal/pkg/outboundidentity"
	"github.com/stretchr/testify/require"
)

func TestOutboundIdentityBrandedCandidatesFallThroughBeforeTransport(t *testing.T) {
	for _, preset := range []string{"claude", "gemini", "grok", "antigravity"} {
		t.Run(preset, func(t *testing.T) {
			bad := OutboundIdentitySelection{Preset: preset, UserAgent: builtInOutboundIdentity(preset).UserAgent + " SuB2ApI-compatible"}
			config := emptyOutboundIdentitySettings()
			config.Profiles[preset] = OutboundIdentitySelection{Preset: preset, Version: "3.9.1"}
			svc, ctx := outboundIdentityTestSettings(t, config)
			require.Error(t, NormalizeAccountOutboundIdentity(PlatformGemini, AccountTypeAPIKey, map[string]any{outboundIdentityCredential: bad}))
			invalidConfig := emptyOutboundIdentitySettings()
			invalidConfig.Profiles[preset] = bad
			require.Error(t, svc.SetOutboundIdentitySettings(ctx, invalidConfig))
			config.Defaults["gemini:apikey"] = preset
			require.NoError(t, svc.SetOutboundIdentitySettings(ctx, config))
			account := &Account{ID: 71, Platform: PlatformGemini, Type: AccountTypeAPIKey, Credentials: map[string]any{outboundIdentityCredential: bad}}
			selected, ok := outboundidentity.FromContext(WithAccountOutboundIdentity(ctx, account))
			require.True(t, ok)
			require.Equal(t, "global", selected.Source)
			require.Equal(t, "3.9.1", selected.Version)
			captured := make(chan http.Header, 1)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				captured <- r.Header.Clone()
				w.WriteHeader(http.StatusNoContent)
			}))
			defer server.Close()
			req, err := http.NewRequestWithContext(ctx, http.MethodGet, server.URL, nil)
			require.NoError(t, err)
			req.Header.Set("Authorization", "Bearer test-token")
			ApplyAccountOutboundIdentity(ctx, account, req)
			client := &http.Client{Transport: brandidentity.WrapRoundTripper(server.Client().Transport)}
			resp, err := client.Do(req)
			require.NoError(t, err)
			require.NoError(t, resp.Body.Close())
			headers := <-captured
			require.Equal(t, selected.UserAgent, headers.Get("User-Agent"))
			for name, value := range selected.Headers {
				require.Equal(t, value, headers.Get(name), name)
			}
			require.Equal(t, "Bearer test-token", headers.Get("Authorization"))
			// Persisted legacy/corrupt global candidates use the compiled fallback.
			svc.outboundIdentityCache.Store(&cachedOutboundIdentitySettings{settings: invalidConfig})
			repo, ok := svc.settingRepo.(*outboundIdentityTestRepo)
			require.True(t, ok)
			repo.values[SettingKeyOutboundIdentity] = `{"profiles":{"` + preset + `":{"preset":"` + preset + `","user_agent":"` + bad.UserAgent + `"}}}`
			fallback := svc.resolveDefaultOutboundIdentity(ctx, preset)
			require.Equal(t, builtInOutboundIdentity(preset), fallback)
		})
	}
}

func TestStandaloneOutboundIdentityOwnsCodexScope(t *testing.T) {
	svc, ctx := outboundIdentityTestSettings(t, emptyOutboundIdentitySettings())
	ctx = WithOutboundIdentityScope(ctx, nil)
	old := svc.resolveDefaultOutboundIdentity(ctx, "codex")
	// Pin a nil-account identity in the forwarding operation, then change global UA.
	repo, ok := svc.settingRepo.(*outboundIdentityTestRepo)
	require.True(t, ok)
	repo.values[SettingKeyOpenAICodexUserAgent] = "codex_cli_rs/0.120.0 (Linux 6.8.0; x86_64) tmux/3.4"
	// Use a fresh settings service to avoid unrelated profile cache lifetimes.
	current := NewSettingService(repo, nil)
	ctx = outboundidentity.WithResolver(ctx, current.resolveOutboundIdentityKey)
	foreign := builtInOutboundIdentity("grok")
	foreign.AccountID = 99
	ctx = outboundidentity.WithIdentity(ctx, foreign)
	selected, ok := outboundidentity.FromContext(WithStandaloneOutboundIdentity(ctx, PlatformOpenAI))
	require.True(t, ok)
	require.Equal(t, "codex", selected.Preset)
	require.Zero(t, selected.AccountID)
	require.Contains(t, selected.UserAgent, "tmux/3.4")
	require.NotEqual(t, old.UserAgent, selected.UserAgent)
	// Standalone calls without runtime wiring still get a complete default.
	ctx = outboundidentity.WithResolver(context.Background(), func(context.Context, string) outboundidentity.Identity { return outboundidentity.Identity{} })
	selected, ok = outboundidentity.FromContext(WithStandaloneOutboundIdentity(ctx, PlatformAnthropic))
	require.True(t, ok)
	require.Equal(t, builtInOutboundIdentity("claude"), selected)
}
