//go:build unit || !integration

package service

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/pkg/deepseek"
	"github.com/LuckyKuang/sub2api-plus/internal/pkg/kimi"
	"github.com/LuckyKuang/sub2api-plus/internal/pkg/minimax"
	"github.com/LuckyKuang/sub2api-plus/internal/pkg/outboundidentity"
	"github.com/LuckyKuang/sub2api-plus/internal/pkg/xai"
	"github.com/LuckyKuang/sub2api-plus/internal/pkg/zcode"
	"github.com/stretchr/testify/require"
)

type outboundIdentityTestRepo struct {
	SettingRepository
	values map[string]string
}

func (r *outboundIdentityTestRepo) GetValue(_ context.Context, key string) (string, error) {
	if v, ok := r.values[key]; ok {
		return v, nil
	}
	return "", ErrSettingNotFound
}
func (r *outboundIdentityTestRepo) GetMultiple(_ context.Context, keys []string) (map[string]string, error) {
	result := map[string]string{}
	for _, key := range keys {
		if v, ok := r.values[key]; ok {
			result[key] = v
		}
	}
	return result, nil
}
func (r *outboundIdentityTestRepo) Set(_ context.Context, key, value string) error {
	r.values[key] = value
	return nil
}
func outboundIdentityTestSettings(t *testing.T, config OutboundIdentitySettings) (*SettingService, context.Context) {
	t.Helper()
	svc := NewSettingService(&outboundIdentityTestRepo{values: map[string]string{}}, nil)
	require.NoError(t, svc.SetOutboundIdentitySettings(context.Background(), config))
	return svc, outboundidentity.WithResolver(context.Background(), svc.resolveOutboundIdentityKey)
}

func TestBuiltInGrokOutboundIdentityMatchesOfficialShell(t *testing.T) {
	t.Setenv(xai.CLIVersionEnv, "")

	require.Equal(t, outboundidentity.Identity{
		Preset:     "grok",
		Source:     "compiled_default",
		UserAgent:  xai.CLIUserAgent(xai.CLIClientVersion),
		Originator: xai.CLIClientIdentifier,
		Version:    xai.CLIClientVersion,
		Headers: map[string]string{
			"User-Agent":               xai.CLIUserAgent(xai.CLIClientVersion),
			"x-grok-client-identifier": xai.CLIClientIdentifier,
			"x-grok-client-version":    xai.CLIClientVersion,
			"x-grok-client-mode":       xai.CLIClientMode,
		},
	}, builtInOutboundIdentity("grok"))
}

func TestPrepareGrokAccountOutboundRequestUsesGrokTransportProfile(t *testing.T) {
	account := &Account{ID: 42, Platform: PlatformGrok, Type: AccountTypeAPIKey, Credentials: map[string]any{}}
	req, err := http.NewRequest(http.MethodPost, "https://api.x.ai/v1/images/generations", nil)
	require.NoError(t, err)

	prepared := prepareAccountOutboundRequest(req, account)

	require.Same(t, req, prepared)
	require.Equal(t, HTTPUpstreamProfileGrok, HTTPUpstreamProfileFromContext(prepared.Context()))
	require.Equal(t, builtInOutboundIdentity("grok").UserAgent, prepared.UserAgent())

	explicit := req.WithContext(WithHTTPUpstreamProfile(req.Context(), HTTPUpstreamProfileLongStream))
	prepareAccountOutboundRequest(explicit, account)
	require.Equal(t, HTTPUpstreamProfileLongStream, HTTPUpstreamProfileFromContext(explicit.Context()))
}

func TestOutboundIdentitySourcePriorityAndAccountTypes(t *testing.T) {
	for _, entry := range []struct{ platform, accountType, preset string }{
		{PlatformAnthropic, AccountTypeOAuth, "claude"}, {PlatformAnthropic, AccountTypeSetupToken, "claude"},
		{PlatformAnthropic, AccountTypeAPIKey, "claude"}, {PlatformAnthropic, AccountTypeBedrock, "claude"},
		{PlatformAnthropic, AccountTypeServiceAccount, "claude"}, {PlatformGemini, AccountTypeOAuth, "gemini"},
		{PlatformGemini, AccountTypeAPIKey, "gemini"}, {PlatformGemini, AccountTypeServiceAccount, "gemini"},
		{PlatformGrok, AccountTypeOAuth, "grok"}, {PlatformGrok, AccountTypeAPIKey, "grok"},
		{PlatformAntigravity, AccountTypeOAuth, "antigravity"}, {PlatformAntigravity, AccountTypeUpstream, "antigravity"},
		{PlatformKimi, AccountTypeAPIKey, "kimi"}, {PlatformZhipu, AccountTypeAPIKey, "zcode"},
		{PlatformZhipu, AccountTypeOAuth, "zcode"},
		{PlatformDeepseek, AccountTypeOAuth, "deepseek"}, {PlatformKimi, AccountTypeOAuth, "kimi"}, {PlatformMiniMax, AccountTypeOAuth, "minimax"},
		{PlatformDeepseek, AccountTypeAPIKey, "deepseek"}, {PlatformMiniMax, AccountTypeAPIKey, "minimax_apikey"},
	} {
		t.Run(entry.platform+"/"+entry.accountType, func(t *testing.T) {
			account := &Account{ID: 42, Platform: entry.platform, Type: entry.accountType, Credentials: map[string]any{}}
			_, ctx := outboundIdentityTestSettings(t, emptyOutboundIdentitySettings())
			got, ok := outboundidentity.FromContext(WithAccountOutboundIdentity(ctx, account))
			require.True(t, ok)
			require.Equal(t, entry.preset, got.Preset)
			require.Equal(t, builtInOutboundIdentity(entry.preset).UserAgent, got.UserAgent)
			if entry.preset == "codex" || entry.preset == minimax.APIKeyPreset {
				return
			} // Codex's existing source matrix has its own complete suite.
			if _, versionless := versionlessOutboundUserAgents[entry.preset]; versionless {
				// Versionless families reject a client-version candidate by
				// design and have their own complete suite below.
				return
			}
			config := emptyOutboundIdentitySettings()
			config.Profiles[entry.preset] = OutboundIdentitySelection{Preset: entry.preset, Version: "3.9.1"}
			svc, ctx := outboundIdentityTestSettings(t, config)
			got, _ = outboundidentity.FromContext(WithAccountOutboundIdentity(ctx, account))
			require.Equal(t, "global", got.Source)
			require.Equal(t, "3.9.1", got.Version)
			account.Credentials[outboundIdentityCredential] = OutboundIdentitySelection{Preset: entry.preset, Version: "3.9.2"}
			got, _ = outboundidentity.FromContext(WithAccountOutboundIdentity(ctx, account))
			require.Equal(t, "account", got.Source)
			require.Equal(t, "3.9.2", got.Version)
			preview, err := svc.PreviewOutboundIdentity(ctx, account, &OutboundIdentitySelection{Preset: entry.preset, Version: "3.9.2"})
			require.NoError(t, err)
			require.Equal(t, preview.Headers, got.Headers)
			account.Credentials[outboundIdentityCredential] = OutboundIdentitySelection{Preset: entry.preset, UserAgent: "inbound/999.0.0", Version: "3.9.3"}
			got, _ = outboundidentity.FromContext(WithAccountOutboundIdentity(ctx, account))
			require.Equal(t, "global", got.Source)
			require.Equal(t, "3.9.1", got.Version, "invalid account candidates fall through as a unit")
		})
	}
}

func TestOutboundIdentityPresetInheritanceSnapshotAndFailover(t *testing.T) {
	config := emptyOutboundIdentitySettings()
	config.Profiles["grok"] = OutboundIdentitySelection{Preset: "grok", Version: "3.9.1"}
	config.Defaults["gemini:apikey"] = "claude"
	svc, ctx := outboundIdentityTestSettings(t, config)
	a := &Account{ID: 1, Platform: PlatformGemini, Type: AccountTypeAPIKey, Credentials: map[string]any{outboundIdentityCredential: OutboundIdentitySelection{Preset: "grok"}}}
	snapshot := WithAccountOutboundIdentity(ctx, a)
	i, _ := outboundidentity.FromContext(snapshot)
	require.Equal(t, "3.9.1", i.Version)
	preview, err := svc.PreviewOutboundIdentity(ctx, a, &OutboundIdentitySelection{Preset: " grok ", Version: " "})
	require.NoError(t, err)
	require.Equal(t, i.UserAgent, preview.UserAgent, "preview uses the same normalized candidate as saving")
	config.Profiles["grok"] = OutboundIdentitySelection{Preset: "grok", Version: "3.9.2"}
	require.NoError(t, svc.SetOutboundIdentitySettings(ctx, config))
	i, _ = outboundidentity.FromContext(WithAccountOutboundIdentity(snapshot, a))
	require.Equal(t, "3.9.1", i.Version, "retries retain the selected identity")
	b := &Account{ID: 2, Platform: PlatformGemini, Type: AccountTypeAPIKey}
	i, _ = outboundidentity.FromContext(WithAccountOutboundIdentity(snapshot, b))
	require.Equal(t, "claude", i.Preset, "failover must resolve the new credential owner")
	i, _ = outboundidentity.FromContext(WithAccountOutboundIdentity(ctx, a))
	require.Equal(t, "3.9.2", i.Version, "new operations see updated settings")
	_, ok := outboundidentity.FromContext(WithAccountOutboundIdentity(snapshot, &Account{ID: 3, Platform: PlatformOpenAI, Type: AccountTypeOAuth}))
	require.False(t, ok, "a non-Codex snapshot must not leak into Codex forwarding")
	_, ok = outboundidentity.FromContext(WithAccountOutboundIdentity(snapshot, &Account{ID: 4, Type: AccountTypeOAuth}))
	require.False(t, ok, "legacy implicit-platform Codex callers keep their existing resolver")
}

func TestOutboundIdentityCodexUnchangedAndCompatibleOptIn(t *testing.T) {
	svc, ctx := outboundIdentityTestSettings(t, emptyOutboundIdentitySettings())
	account := &Account{ID: 1, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Credentials: map[string]any{"user_agent": DefaultOpenAICodexUserAgent}}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://example.com/v1/responses", nil)
	require.NoError(t, err)
	identity := resolveOpenAIOutboundIdentityFromSettings(ctx, account, svc)
	applyResolvedOpenAIOutboundIdentity(req.Header, identity, false)
	before := req.Header.Clone()
	prepareAccountOutboundRequest(req, account)
	require.Equal(t, before, req.Header)
	account.Credentials[outboundIdentityCredential] = OutboundIdentitySelection{Preset: "claude"}
	prepareAccountOutboundRequest(req, account)
	require.Equal(t, before, req.Header, "the current request keeps its selected Codex identity")
	req, err = http.NewRequestWithContext(ctx, http.MethodPost, "https://example.com/v1/responses", nil)
	require.NoError(t, err)
	prepareAccountOutboundRequest(req, account)
	require.Equal(t, builtInOutboundIdentity("claude").UserAgent, req.Header.Get("User-Agent"))
	require.Empty(t, req.Header.Get("Originator"))
	require.Equal(t, "cli", req.Header.Get("X-App"))
}

func TestOutboundIdentityValidationAndVersionOnlyChange(t *testing.T) {
	for _, preset := range outboundPresetNames {
		if preset == minimax.APIKeyPreset {
			continue
		} // SDK pin has a dedicated rejection regression.
		if _, versionless := versionlessOutboundUserAgents[preset]; versionless {
			// A versionless family publishes no client version, so there is no
			// version-only change to assert; TestMiniMaxOutboundIdentity-
			// VersionlessExemptionIsNarrow owns its rejection behavior.
			continue
		}
		before := builtInOutboundIdentity(preset)
		after, err := buildOutboundIdentity(OutboundIdentitySelection{Preset: preset, UserAgent: before.UserAgent, Version: "3.9.1"})
		require.NoError(t, err, preset)
		require.Equal(t, before.Originator, after.Originator)
		require.Equal(t, before.Inference, after.Inference, "version changes preserve protocol SDK fingerprints")
		require.Equal(t, before.ControlHeaders, after.ControlHeaders, "version changes preserve control-plane host facts")
		require.Equal(t, strings.Replace(before.UserAgent, "/"+before.Version, "/3.9.1", 1), after.UserAgent)
		for key, value := range before.Headers {
			if key == "User-Agent" || outboundHeaderClassName(preset, key) == outboundHeaderDerived {
				// Derived declarations follow the resolved triple; the coherence
				// assertions below own their exact post-change values.
				continue
			}
			require.Equal(t, value, after.Headers[key], key)
		}
		if preset == "kimi" {
			// The version companion must follow the resolved triple, while the
			// platform token and the deployment-owned device set must not move.
			require.Equal(t, "3.9.1", after.Headers[kimi.HeaderVersion], "X-Msh-Version")
			require.Equal(t, kimi.PlatformToken, after.Headers[kimi.HeaderPlatform], "X-Msh-Platform")
			for _, name := range kimi.DeviceHeaders() {
				require.Equal(t, before.Headers[name], after.Headers[name], name)
			}
		}
	}
	for _, selection := range []OutboundIdentitySelection{{Preset: "unknown"}, {Preset: "claude", UserAgent: "claude-cli/3.9.1\r\nAuthorization: secret"}, {Preset: "gemini", UserAgent: strings.Repeat("x", 513)}, {Preset: "grok", Version: "invalid"}, {Preset: "deepseek", UserAgent: "deepseek/0.2.0-rc.2"}, {Preset: "deepseek", UserAgent: "deepseek-harness/0.2.0-rc.2 (sub2api)"}, {Preset: "deepseek", Version: "0.0.1"}, {Preset: "minimax", UserAgent: "minimax/0.6.2"}, {Preset: "minimax", UserAgent: "MiniMaxAgent/0.6.2"}, {Preset: "minimax", UserAgent: "MiniMaxAgent (sub2api)"}, {Preset: "minimax", Version: "0.6.2"}, {Preset: "zcode", UserAgent: "zcode/3.14.3"}, {Preset: "zcode", UserAgent: "ZCode"}, {Preset: "zcode", UserAgent: "ZCode/3.14"}, {Preset: "zcode", UserAgent: "ZCode/3.14.3 (sub2api)"}, {Preset: "zcode", Version: "invalid"}} {
		_, err := buildOutboundIdentity(selection)
		require.Error(t, err)
	}
	credentials := map[string]any{outboundIdentityCredential: OutboundIdentitySelection{Preset: "grok"}}
	require.Error(t, NormalizeAccountOutboundIdentity(PlatformGemini, AccountTypeOAuth, credentials))
	require.Error(t, NormalizeAccountOutboundIdentity(PlatformGemini, AccountTypeServiceAccount, credentials))
	credentials[outboundIdentityCredential] = nil
	require.NoError(t, NormalizeAccountOutboundIdentity(PlatformGemini, AccountTypeServiceAccount, credentials))
	require.NotContains(t, credentials, outboundIdentityCredential)
}

func TestOutboundIdentitySettingsPersistAndDoNotExposeMutableCache(t *testing.T) {
	svc, ctx := outboundIdentityTestSettings(t, emptyOutboundIdentitySettings())
	config := emptyOutboundIdentitySettings()
	config.Defaults["anthropic:oauth"] = "grok"
	require.Error(t, svc.SetOutboundIdentitySettings(ctx, config))
	config.Profiles["grok"] = OutboundIdentitySelection{
		Preset: "ignored", UserAgent: "  ", Version: " 3.9.1 ",
	}
	config.Defaults = map[string]string{"gemini:apikey": " grok "}
	require.NoError(t, svc.SetOutboundIdentitySettings(ctx, config))
	config.Defaults["gemini:apikey"] = "claude"
	view := svc.GetOutboundIdentitySettings(ctx)
	require.Equal(t, "grok", view.Defaults["gemini:apikey"])
	require.Equal(t, OutboundIdentitySelection{Preset: "grok", Version: "3.9.1"}, view.Profiles["grok"])
	view.Defaults["gemini:apikey"] = "claude"
	require.Equal(t, "grok", svc.GetOutboundIdentitySettings(ctx).Defaults["gemini:apikey"])
	raw, err := svc.settingRepo.GetValue(ctx, SettingKeyOutboundIdentity)
	require.NoError(t, err)
	var persisted OutboundIdentitySettings
	require.NoError(t, json.Unmarshal([]byte(raw), &persisted))
	require.Equal(t, "grok", persisted.Defaults["gemini:apikey"])
	require.Equal(t, OutboundIdentitySelection{Preset: "grok", Version: "3.9.1"}, persisted.Profiles["grok"])
}

func TestOutboundIdentityImportsLegacyAntigravitySettingOnlyUntilFirstSave(t *testing.T) {
	repo := &outboundIdentityTestRepo{values: map[string]string{SettingKeyAntigravityUserAgentVersion: "3.9.1"}}
	svc := NewSettingService(repo, nil)
	ctx := context.Background()
	require.Equal(t, "3.9.1", svc.GetOutboundIdentitySettings(ctx).Profiles["antigravity"].Version)
	require.Equal(t, "3.9.1", svc.resolveDefaultOutboundIdentity(ctx, "antigravity").Version)
	require.NoError(t, svc.SetOutboundIdentitySettings(ctx, emptyOutboundIdentitySettings()))
	restarted := NewSettingService(repo, nil)
	require.Empty(t, restarted.GetOutboundIdentitySettings(ctx).Profiles)
	require.Equal(t, builtInOutboundIdentity("antigravity").Version, restarted.resolveDefaultOutboundIdentity(ctx, "antigravity").Version)
}

func TestOutboundIdentityBedrockSigningRetainsSelectedDeclarations(t *testing.T) {
	for _, preset := range outboundPresetNames {
		t.Run(preset, func(t *testing.T) {
			account := &Account{ID: 1, Platform: PlatformAnthropic, Type: AccountTypeBedrock, Credentials: map[string]any{outboundIdentityCredential: OutboundIdentitySelection{Preset: preset}}}
			ctx := WithAccountOutboundIdentity(context.Background(), account)
			svc := &GatewayService{}
			signer := NewBedrockSigner("test-key", "test-secret", "test-session", "us-east-1")
			body := []byte(`{"messages":[{"role":"user","content":"hello"}],"max_tokens":1}`)
			req, err := svc.buildUpstreamRequestBedrock(ctx, body, "anthropic.claude-sonnet", "us-east-1", false, signer)
			require.NoError(t, err)
			require.Equal(t, builtInOutboundIdentity("claude").UserAgent, req.Header.Get("User-Agent"))
			require.Contains(t, req.Header.Get("Authorization"), "AWS4-HMAC-SHA256")
			before := req.Header.Clone()
			prepareAccountOutboundRequest(req, account)
			require.Equal(t, before, req.Header, "send-time application must preserve every signed header")
			// Independently re-sign the final wire headers at the original timestamp.
			stamp, err := time.Parse("20060102T150405Z", req.Header.Get("X-Amz-Date"))
			require.NoError(t, err)
			final := req.Clone(ctx)
			final.Header.Del("Authorization")
			require.NoError(t, signer.signer.SignHTTP(ctx, signer.credentials, final, sha256Hash(body), "bedrock", "us-east-1", stamp))
			require.Equal(t, before.Get("Authorization"), final.Header.Get("Authorization"))
		})
	}
}

// The DeepSeek preset pins the published harness fingerprint. The parenthesized
// `+url` comment belongs to the same User-Agent value, so the exact string is
// asserted rather than a prefix.
func TestBuiltInDeepSeekOutboundIdentityPinsPublishedHarness(t *testing.T) {
	t.Setenv(deepseek.VersionEnv, "")

	const pinnedUA = "deepseek-harness/0.2.0-rc.2 (+https://github.com/deepseek-ai/deepseek-harness)"
	require.Equal(t, outboundidentity.Identity{
		Preset:     "deepseek",
		Language:   "zh-CN",
		Timezone:   "UTC",
		Source:     "compiled_default",
		UserAgent:  pinnedUA,
		Originator: "deepseek-harness",
		Version:    "0.2.0-rc.2",
		Headers:    map[string]string{"User-Agent": pinnedUA},
	}, builtInOutboundIdentity("deepseek"))
	require.Equal(t, builtInOutboundIdentity("deepseek"), deepseek.DefaultIdentity())
	require.Equal(t, pinnedUA, deepseek.UserAgent(deepseek.DefaultVersion))
}

func TestDeepSeekOutboundIdentityEnvironmentOverrideUsesSupportedVersionsOnly(t *testing.T) {
	t.Setenv(deepseek.VersionEnv, "0.3.0")
	configured := builtInOutboundIdentity("deepseek")
	require.Equal(t, "environment", configured.Source)
	require.Equal(t, "0.3.0", configured.Version)
	require.Equal(t, "deepseek-harness/0.3.0 (+https://github.com/deepseek-ai/deepseek-harness)", configured.UserAgent)
	require.Equal(t, map[string]string{"User-Agent": configured.UserAgent}, configured.Headers)

	for _, invalid := range []string{"", "not-a-version", "0.0.1", "0.2"} {
		t.Setenv(deepseek.VersionEnv, invalid)
		fallback := builtInOutboundIdentity("deepseek")
		require.Equal(t, "compiled_default", fallback.Source, invalid)
		require.Equal(t, deepseek.DefaultVersion, fallback.Version, invalid)
	}
}

// A DeepSeek account that selects the preset must render only the User-Agent
// declaration. Codex's Originator/Version stay off the wire, while protocol
// request state such as the harness session headers keeps its own ownership.
func TestDeepSeekOutboundIdentityRendersOnlyUserAgent(t *testing.T) {
	_, ctx := outboundIdentityTestSettings(t, emptyOutboundIdentitySettings())
	account := &Account{ID: 7, Platform: PlatformDeepseek, Type: AccountTypeAPIKey, Credentials: map[string]any{
		outboundIdentityCredential: OutboundIdentitySelection{Preset: "deepseek"},
	}}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.deepseek.com/v1/chat/completions", nil)
	require.NoError(t, err)
	req.Header.Set("Originator", "codex_cli_rs")
	req.Header.Set("Version", "0.158.0")
	req.Header.Set("X-DeepSeek-Harness-User-Id", "request-state")

	prepareAccountOutboundRequest(req, account)

	require.Equal(t, deepseek.UserAgent(deepseek.DefaultVersion), req.Header.Get("User-Agent"))
	require.Empty(t, req.Header.Get("Originator"))
	require.Empty(t, req.Header.Get("Version"))
	require.Equal(t, "request-state", req.Header.Get("X-DeepSeek-Harness-User-Id"), "request state is not an identity declaration")
}

// DeepSeek platform accounts advertise the pinned harness identity by default.
// This test records the native default and rejects retired mappings and foreign account candidates.
func TestDeepSeekDefaultIdentityIsPinnedHarness(t *testing.T) {
	require.Equal(t, "deepseek", nativeOutboundPreset(PlatformDeepseek))

	account := &Account{ID: 9, Platform: PlatformDeepseek, Type: AccountTypeAPIKey, Credentials: map[string]any{}}
	_, ctx := outboundIdentityTestSettings(t, emptyOutboundIdentitySettings())
	got, ok := outboundidentity.FromContext(WithAccountOutboundIdentity(ctx, account))
	require.True(t, ok)
	require.Equal(t, "deepseek", got.Preset)
	require.Equal(t, "compiled_default", got.Source)
	require.Equal(t, deepseek.UserAgent(deepseek.DefaultVersion), got.UserAgent)
	require.Equal(t, deepseek.ClientIdentifier, got.Originator)
	require.Equal(t, deepseek.DefaultVersion, got.Version)
	require.Equal(t, map[string]string{"User-Agent": got.UserAgent}, got.Headers)

	// Native families require no mapping; even a redundant mapping is rejected.
	config := emptyOutboundIdentitySettings()
	config.Defaults["deepseek:apikey"] = "deepseek"
	svc, pinnedCtx := outboundIdentityTestSettings(t, emptyOutboundIdentitySettings())
	require.Error(t, svc.SetOutboundIdentitySettings(pinnedCtx, config))
	got, ok = outboundidentity.FromContext(WithAccountOutboundIdentity(pinnedCtx, account))
	require.True(t, ok)
	require.Equal(t, "deepseek", got.Preset)
	require.Equal(t, "compiled_default", got.Source)

	// A persisted foreign candidate falls through atomically to the native family.
	override := &Account{ID: 10, Platform: PlatformDeepseek, Type: AccountTypeAPIKey, Credentials: map[string]any{
		outboundIdentityCredential: OutboundIdentitySelection{Preset: "codex"},
	}}
	got, ok = outboundidentity.FromContext(WithAccountOutboundIdentity(ctx, override))
	require.True(t, ok)
	require.Equal(t, "deepseek", got.Preset)
	require.Equal(t, "compiled_default", got.Source)
}

// The MiniMax preset pins the published MiniMax Code product declaration. The
// official client family renders the bare token `MiniMaxAgent` and never puts a
// version segment on the wire, so the exact string is asserted rather than a
// versioned prefix, and Version stays intentionally empty.
func TestBuiltInMiniMaxOutboundIdentityPinsProductToken(t *testing.T) {
	const pinnedUA = "MiniMaxAgent"
	require.Equal(t, outboundidentity.Identity{
		Preset:     "minimax",
		Timezone:   "UTC",
		Source:     "compiled_default",
		UserAgent:  pinnedUA,
		Originator: pinnedUA,
		Version:    "",
		Headers:    map[string]string{"User-Agent": pinnedUA, "X-Stainless-Lang": "js", "X-Stainless-Package-Version": "0.91.1", "X-Stainless-OS": "Linux", "X-Stainless-Arch": "x64", "X-Stainless-Runtime": "node", "X-Stainless-Runtime-Version": "v22.19.0"},
	}, builtInOutboundIdentity("minimax"))
	require.Equal(t, builtInOutboundIdentity("minimax"), minimax.DefaultIdentity())
	require.Equal(t, pinnedUA, minimax.UserAgent())
}

// The versionless exemption is enumerated and narrow: it accepts only the
// compiled product token, rejects an invented client version, and cannot be
// borrowed by an unlisted preset.
func TestMiniMaxOutboundIdentityVersionlessExemptionIsNarrow(t *testing.T) {
	accepted, err := buildOutboundIdentity(OutboundIdentitySelection{Preset: "minimax"})
	require.NoError(t, err)
	require.Equal(t, minimax.ProductToken, accepted.UserAgent)
	require.Empty(t, accepted.Version)

	accepted, err = buildOutboundIdentity(OutboundIdentitySelection{Preset: "minimax", UserAgent: minimax.ProductToken})
	require.NoError(t, err)
	require.Equal(t, minimax.ProductToken, accepted.UserAgent)
	require.Empty(t, accepted.Version)

	// The exemption is registered for exactly one preset.
	require.Equal(t, map[string]string{"minimax": "MiniMaxAgent", "stepfun": "step (linux 6.8.0-31-generic; x64)"}, versionlessOutboundUserAgents)
	for _, preset := range outboundPresetNames {
		require.Equal(t, (preset == "minimax" || preset == "stepfun"), versionlessOutboundUserAgents[preset] != "", preset)
	}

	credentials := map[string]any{outboundIdentityCredential: OutboundIdentitySelection{Preset: "minimax"}}
	require.Error(t, NormalizeAccountOutboundIdentity(PlatformMiniMax, AccountTypeAPIKey, credentials))
	require.NoError(t, NormalizeAccountOutboundIdentity(PlatformMiniMax, AccountTypeOAuth, credentials))
	require.Equal(t, OutboundIdentitySelection{Preset: "minimax"}, credentials[outboundIdentityCredential])
}

// A MiniMax account that selects the preset must render only the User-Agent
// declaration. Codex's Originator/Version stay off the wire, while protocol
// request state such as the MiniMax session headers keeps its own ownership.
func TestMiniMaxOutboundIdentityRendersOfficialSDKAndSessionHeaders(t *testing.T) {
	_, ctx := outboundIdentityTestSettings(t, emptyOutboundIdentitySettings())
	account := &Account{ID: 8, Platform: PlatformMiniMax, Type: AccountTypeOAuth, Credentials: map[string]any{
		outboundIdentityCredential: OutboundIdentitySelection{Preset: "minimax"},
	}}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.minimaxi.com/anthropic/v1/messages", nil)
	require.NoError(t, err)
	req.Header.Set("Originator", "codex_cli_rs")
	req.Header.Set("Version", "0.158.0")
	req.Header.Set("X-Mavis-Session-Id", "request-state")
	req.Header.Set("Anthropic-Version", "2023-06-01")

	prepareAccountOutboundRequest(req, account)

	require.Equal(t, minimax.ProductToken, req.Header.Get("User-Agent"))
	require.Empty(t, req.Header.Get("Originator"))
	require.Empty(t, req.Header.Get("Version"))
	require.NotEmpty(t, req.Header.Get("X-Mavis-Session-Id"))
	require.NotEqual(t, "request-state", req.Header.Get("X-Mavis-Session-Id"), "protocol layer owns session attribution")
	require.Equal(t, "2023-06-01", req.Header.Get("Anthropic-Version"), "protocol versions are not identity declarations")
}

// MiniMax platform accounts advertise the pinned product identity by default.
// This test records the API-key SDK default and rejects foreign account candidates.
func TestMiniMaxAPIKeyDefaultIdentityIsPinnedSDK(t *testing.T) {
	require.Equal(t, "minimax", nativeOutboundPreset(PlatformMiniMax))

	account := &Account{ID: 11, Platform: PlatformMiniMax, Type: AccountTypeAPIKey, Credentials: map[string]any{}}
	_, ctx := outboundIdentityTestSettings(t, emptyOutboundIdentitySettings())
	got, ok := outboundidentity.FromContext(WithAccountOutboundIdentity(ctx, account))
	require.True(t, ok)
	require.Equal(t, "minimax_apikey", got.Preset)
	require.Equal(t, "compiled_default", got.Source)
	require.Equal(t, "Anthropic/JS 0.91.1", got.UserAgent)
	require.Equal(t, "Anthropic", got.Originator)
	require.Equal(t, "0.91.1", got.Version)
	require.Equal(t, minimax.APIKeyIdentity().Headers, got.Headers)

	// Native families require no mapping; even a redundant mapping is rejected.
	config := emptyOutboundIdentitySettings()
	config.Defaults["minimax:apikey"] = "minimax_apikey"
	svc, pinnedCtx := outboundIdentityTestSettings(t, emptyOutboundIdentitySettings())
	require.Error(t, svc.SetOutboundIdentitySettings(pinnedCtx, config))
	got, ok = outboundidentity.FromContext(WithAccountOutboundIdentity(pinnedCtx, account))
	require.True(t, ok)
	require.Equal(t, "minimax_apikey", got.Preset)
	require.Equal(t, "compiled_default", got.Source)

	// A persisted foreign candidate falls through atomically to the native family.
	override := &Account{ID: 12, Platform: PlatformMiniMax, Type: AccountTypeAPIKey, Credentials: map[string]any{
		outboundIdentityCredential: OutboundIdentitySelection{Preset: "codex"},
	}}
	got, ok = outboundidentity.FromContext(WithAccountOutboundIdentity(ctx, override))
	require.True(t, ok)
	require.Equal(t, "minimax_apikey", got.Preset)
	require.Equal(t, "compiled_default", got.Source)
}

func TestBuiltInZCodeOutboundIdentityPinsProductVersion(t *testing.T) {
	t.Setenv(zcode.VersionEnv, "")
	got := builtInOutboundIdentity("zcode")
	require.Equal(t, "ZCode/3.14.3", got.UserAgent)
	require.Equal(t, "ZCode", got.Originator)
	require.Equal(t, "3.14.3", got.Version)
	require.Equal(t, "compiled_default", got.Source)
	require.Equal(t, got.Version, got.Headers[zcode.HeaderAppVersion])
	require.Equal(t, "https://zcode.z.ai", got.Headers["HTTP-Referer"])
	require.Equal(t, "Z Code@electron", got.Headers["X-Title"])
	require.Equal(t, "production", got.Headers["X-Release-Channel"])
	require.Equal(t, "glm", got.Headers["X-ZCode-Agent"])
	require.Len(t, got.Headers, 11)
	require.Equal(t, got, zcode.DefaultIdentity())
}

// ZCode ships two parallel official version lines: the desktop / server product
// version and the standalone CLI package version. The override therefore accepts
// both and declares no monotonic version floor.
func TestZCodeOutboundIdentityEnvironmentOverrideAcceptsBothOfficialLines(t *testing.T) {
	for _, version := range []string{"3.14.3", "0.16.9", "3.15.0-rc.1"} {
		t.Setenv(zcode.VersionEnv, version)
		configured := builtInOutboundIdentity("zcode")
		require.Equal(t, "environment", configured.Source, version)
		require.Equal(t, version, configured.Version)
		require.Equal(t, "ZCode/"+version, configured.UserAgent)
		require.Equal(t, configured.Version, configured.Headers[zcode.HeaderAppVersion])
	}

	for _, invalid := range []string{"", "not-a-version", "3.14", "3.14.3.1", "3.14.3 x"} {
		t.Setenv(zcode.VersionEnv, invalid)
		fallback := builtInOutboundIdentity("zcode")
		require.Equal(t, "compiled_default", fallback.Source, invalid)
		require.Equal(t, zcode.DefaultVersion, fallback.Version, invalid)
	}
}

// ZCode companions replace foreign declarations while protocol versions survive.
func TestZCodeOutboundIdentityRendersOfficialDeclarations(t *testing.T) {
	_, ctx := outboundIdentityTestSettings(t, emptyOutboundIdentitySettings())
	account := &Account{ID: 13, Platform: PlatformZhipu, Type: AccountTypeAPIKey, Credentials: map[string]any{
		outboundIdentityCredential: OutboundIdentitySelection{Preset: "zcode"},
	}}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://open.bigmodel.cn/api/anthropic/v1/messages", nil)
	require.NoError(t, err)
	req.Header.Set("Originator", "codex_cli_rs")
	req.Header.Set("Version", "0.158.0")
	req.Header.Set("X-ZCode-App-Version", "request-state")
	req.Header.Set("Anthropic-Version", "2023-06-01")

	prepareAccountOutboundRequest(req, account)

	require.Equal(t, zcode.DefaultIdentity().ForProtocol("anthropic").UserAgent, req.Header.Get("User-Agent"))
	require.Empty(t, req.Header.Get("Originator"))
	require.Empty(t, req.Header.Get("Version"))
	require.Equal(t, zcode.DefaultVersion, req.Header.Get("X-ZCode-App-Version"))
	require.Equal(t, "2023-06-01", req.Header.Get("Anthropic-Version"), "protocol versions are not identity declarations")
}

// Zhipu / GLM platform accounts advertise the pinned ZCode identity by default,
// for both API-key and account-link OAuth accounts. This test is the audit record
// for the native default, rejected mappings and atomic fallback of foreign candidates.
func TestZCodeDefaultIdentityIsPinnedProduct(t *testing.T) {
	require.Equal(t, "zcode", nativeOutboundPreset(PlatformZhipu))

	for _, accountType := range []string{AccountTypeAPIKey, AccountTypeOAuth} {
		account := &Account{ID: 14, Platform: PlatformZhipu, Type: accountType, Credentials: map[string]any{}}
		_, ctx := outboundIdentityTestSettings(t, emptyOutboundIdentitySettings())
		got, ok := outboundidentity.FromContext(WithAccountOutboundIdentity(ctx, account))
		require.True(t, ok, accountType)
		require.Equal(t, "zcode", got.Preset, accountType)
		require.Equal(t, "compiled_default", got.Source, accountType)
		require.Equal(t, zcode.UserAgent(zcode.DefaultVersion), got.UserAgent, accountType)
		require.Equal(t, zcode.ProductToken, got.Originator, accountType)
		require.Equal(t, zcode.DefaultVersion, got.Version, accountType)
		require.Equal(t, zcode.DefaultIdentity().Headers, got.Headers, accountType)
	}

	// Native families require no mapping; even a redundant mapping is rejected.
	account := &Account{ID: 15, Platform: PlatformZhipu, Type: AccountTypeAPIKey, Credentials: map[string]any{}}
	config := emptyOutboundIdentitySettings()
	config.Defaults["zhipu:apikey"] = "zcode"
	svc, pinnedCtx := outboundIdentityTestSettings(t, emptyOutboundIdentitySettings())
	require.Error(t, svc.SetOutboundIdentitySettings(pinnedCtx, config))
	got, ok := outboundidentity.FromContext(WithAccountOutboundIdentity(pinnedCtx, account))
	require.True(t, ok)
	require.Equal(t, "zcode", got.Preset)
	require.Equal(t, "compiled_default", got.Source)

	// API-key accounts retain the native family even with a stale foreign candidate.
	_, ctx := outboundIdentityTestSettings(t, emptyOutboundIdentitySettings())
	override := &Account{ID: 16, Platform: PlatformZhipu, Type: AccountTypeAPIKey, Credentials: map[string]any{
		outboundIdentityCredential: OutboundIdentitySelection{Preset: "codex"},
	}}
	got, ok = outboundidentity.FromContext(WithAccountOutboundIdentity(ctx, override))
	require.True(t, ok)
	require.Equal(t, "zcode", got.Preset)
	require.Equal(t, "compiled_default", got.Source)

	// An OAuth account is pinned to its native family and cannot opt out; the
	// selection is rejected and the account keeps the ZCode identity.
	oauthOverride := &Account{ID: 17, Platform: PlatformZhipu, Type: AccountTypeOAuth, Credentials: map[string]any{
		outboundIdentityCredential: OutboundIdentitySelection{Preset: "codex"},
	}}
	got, ok = outboundidentity.FromContext(WithAccountOutboundIdentity(ctx, oauthOverride))
	require.True(t, ok)
	require.Equal(t, "zcode", got.Preset, "OAuth accounts must retain their native client family")
}

// Kimi Code publishes one product token plus a device description set on every
// first-party provider request and declares no Originator and no standalone
// Version header. This suite is the audit record for the pinned declaration
// set, for the runtime device tier the settings own, and for the account tier
// that overrides it.
func TestBuiltInKimiOutboundIdentityMatchesOfficialClient(t *testing.T) {
	t.Setenv(kimi.VersionEnv, "")

	pinned := builtInOutboundIdentity("kimi")
	require.Equal(t, "kimi", pinned.Preset)
	require.Equal(t, "compiled_default", pinned.Source)
	require.Equal(t, "kimi-code-cli/2.1.1", pinned.UserAgent)
	require.Equal(t, kimi.ProductToken, pinned.Originator)
	require.Equal(t, kimi.DefaultVersion, pinned.Version)
	require.Equal(t, pinned, kimi.DefaultIdentity())

	// The complete official declaration block, and nothing beyond it.
	require.Len(t, pinned.Headers, 7)
	require.Equal(t, pinned.UserAgent, pinned.Headers["User-Agent"])
	require.Equal(t, kimi.PlatformToken, pinned.Headers[kimi.HeaderPlatform])
	require.Equal(t, pinned.Version, pinned.Headers[kimi.HeaderVersion])
	require.NotContains(t, pinned.Headers, "Originator")
	require.NotContains(t, pinned.Headers, "Version")
	for _, name := range kimi.DeviceHeaders() {
		require.NotEmpty(t, pinned.Headers[name], name)
	}
}

func TestKimiOutboundIdentityRendersTheOfficialHeaderSet(t *testing.T) {
	_, ctx := outboundIdentityTestSettings(t, emptyOutboundIdentitySettings())
	account := &Account{ID: 51, Platform: PlatformKimi, Type: AccountTypeAPIKey, Credentials: map[string]any{}}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.kimi.com/coding/v1/chat/completions", nil)
	require.NoError(t, err)
	// Request state a caller must never be able to select an identity with.
	req.Header.Set("User-Agent", "caller/1.0")
	req.Header.Set("Originator", "codex_cli_rs")
	req.Header.Set("Version", "0.158.0")
	req.Header.Set(kimi.HeaderDeviceID, "11111111-1111-4111-8111-111111111111")
	req.Header.Set("X-Msh-Tool-Call-Id", "request-state")
	req.Header.Set("Authorization", "Bearer credential")

	prepareAccountOutboundRequest(req, account)

	identity := builtInOutboundIdentity("kimi")
	require.Equal(t, identity.UserAgent, req.Header.Get("User-Agent"))
	for name, value := range identity.Headers {
		if name == "User-Agent" {
			continue
		}
		require.Equal(t, value, req.Header.Get(name), name)
	}
	require.Empty(t, req.Header.Get("Originator"), "the official client declares no Originator")
	require.Empty(t, req.Header.Get("Version"), "the official client declares no standalone Version header")
	require.Equal(t, "request-state", req.Header.Get("X-Msh-Tool-Call-Id"), "a tool-call id is request state, not identity")
	require.Equal(t, "Bearer credential", req.Header.Get("Authorization"))
}

func TestKimiDefaultIdentityIsPinnedProduct(t *testing.T) {
	require.Equal(t, "kimi", nativeOutboundPreset(PlatformKimi))

	_, ctx := outboundIdentityTestSettings(t, emptyOutboundIdentitySettings())
	account := &Account{ID: 52, Platform: PlatformKimi, Type: AccountTypeAPIKey, Credentials: map[string]any{}}
	got, ok := outboundidentity.FromContext(WithAccountOutboundIdentity(ctx, account))
	require.True(t, ok)
	require.Equal(t, "kimi", got.Preset)
	require.Equal(t, kimi.UserAgent(kimi.DefaultVersion), got.UserAgent)
	require.Equal(t, kimi.ProductToken, got.Originator)
	require.Equal(t, kimi.DefaultVersion, got.Version)

	// Native families require no mapping; even a redundant mapping is rejected.
	config := emptyOutboundIdentitySettings()
	config.Defaults["kimi:apikey"] = "kimi"
	svc, pinnedCtx := outboundIdentityTestSettings(t, emptyOutboundIdentitySettings())
	require.Error(t, svc.SetOutboundIdentitySettings(pinnedCtx, config))
	got, ok = outboundidentity.FromContext(WithAccountOutboundIdentity(pinnedCtx, account))
	require.True(t, ok)
	require.Equal(t, "kimi", got.Preset)

	// A persisted foreign candidate falls through atomically to the native family.
	override := &Account{ID: 53, Platform: PlatformKimi, Type: AccountTypeAPIKey, Credentials: map[string]any{
		outboundIdentityCredential: OutboundIdentitySelection{Preset: "codex"},
	}}
	got, ok = outboundidentity.FromContext(WithAccountOutboundIdentity(ctx, override))
	require.True(t, ok)
	require.Equal(t, "kimi", got.Preset)
	require.Equal(t, "compiled_default", got.Source)
}

func TestKimiVersionCandidateStaysInsideTheNamespace(t *testing.T) {
	for _, version := range []string{"2.1", "2.0.9", "v2.1.1"} {
		_, err := buildOutboundIdentity(OutboundIdentitySelection{Preset: "kimi", Version: version})
		require.Error(t, err, version)
	}
	for _, userAgent := range []string{"kimi-code-cli", "Kimi/2.1.1", "kimi/2.1.1", "kimi-code-cli/2.1.1 (sub2api)", "kimi-code-cli/2.1.1/extra", "codex_cli_rs/0.158.0"} {
		_, err := buildOutboundIdentity(OutboundIdentitySelection{Preset: "kimi", UserAgent: userAgent})
		require.Error(t, err, userAgent)
	}
	identity, err := buildOutboundIdentity(OutboundIdentitySelection{Preset: "kimi", Version: "2.4.0"})
	require.NoError(t, err)
	require.Equal(t, "kimi-code-cli/2.4.0", identity.UserAgent)
	require.Equal(t, "2.4.0", identity.Headers[kimi.HeaderVersion])
	require.Equal(t, kimi.PlatformToken, identity.Headers[kimi.HeaderPlatform])
}

// The official client resolves the device set from the machine it runs on. A
// gateway has no per-user device, so the deployment resolves it once from its
// own host, persists it in the outbound identity settings, and lets an operator
// or an account override every value.
func TestKimiRuntimeDeclarationsAreMaterializedOnceAndStayStable(t *testing.T) {
	svc, ctx := outboundIdentityTestSettings(t, emptyOutboundIdentitySettings())
	require.Empty(t, svc.GetOutboundIdentitySettings(ctx).Runtime)

	svc.ensureRuntimeOutboundHeaders(ctx)

	persisted := svc.GetOutboundIdentitySettings(ctx).Runtime["kimi"]
	require.Len(t, persisted, 2)
	for _, name := range []string{kimi.HeaderDeviceName, kimi.HeaderDeviceID} {
		require.Equal(t, builtInOutboundIdentity("kimi").Headers[name], persisted[name], name)
	}

	// A second materialization is a no-op: the persisted identity must not churn.
	svc.ensureRuntimeOutboundHeaders(ctx)
	require.Equal(t, persisted, svc.GetOutboundIdentitySettings(ctx).Runtime["kimi"])

	account := &Account{ID: 61, Platform: PlatformKimi, Type: AccountTypeAPIKey, Credentials: map[string]any{}}
	got, ok := outboundidentity.FromContext(WithAccountOutboundIdentity(ctx, account))
	require.True(t, ok)
	require.Equal(t, "global", got.Source, "persisted runtime values are global settings, not the compiled default")
	require.Equal(t, persisted[kimi.HeaderDeviceID], got.Headers[kimi.HeaderDeviceID])
	require.Equal(t, kimi.PlatformToken, got.Headers[kimi.HeaderPlatform], "a pinned declaration never moves")
}

func TestKimiRuntimeDeclarationsAcceptOnlyRuntimeClassNames(t *testing.T) {
	svc, ctx := outboundIdentityTestSettings(t, emptyOutboundIdentitySettings())
	builtin := builtInOutboundIdentity("kimi")

	config := emptyOutboundIdentitySettings()
	config.Runtime = map[string]map[string]string{"kimi": {
		kimi.HeaderDeviceName: "kimi-gateway-1",
		kimi.HeaderDeviceID:   "22222222-2222-4222-8222-222222222222",
	}}
	require.NoError(t, svc.SetOutboundIdentitySettings(ctx, config))

	resolved := svc.resolveDefaultOutboundIdentity(ctx, "kimi")
	require.Equal(t, "global", resolved.Source)
	require.Equal(t, "kimi-gateway-1", resolved.Headers[kimi.HeaderDeviceName])
	require.Equal(t, "22222222-2222-4222-8222-222222222222", resolved.Headers[kimi.HeaderDeviceID])
	require.Equal(t, builtin.Headers["User-Agent"], resolved.Headers["User-Agent"])

	for name, value := range map[string]string{
		kimi.HeaderPlatform:  "kimi_code_desktop",
		kimi.HeaderVersion:   "9.9.9",
		"X-Msh-Tool-Call-Id": "invented",
		"User-Agent":         "kimi-code-cli/9.9.9",
		kimi.HeaderDeviceID:  "not-a-uuid",
		kimi.HeaderOSVersion: "line\r\nbreak",
	} {
		rejected := emptyOutboundIdentitySettings()
		rejected.Runtime = map[string]map[string]string{"kimi": {name: value}}
		require.Error(t, svc.SetOutboundIdentitySettings(ctx, rejected), name)
	}

	// An unknown preset cannot introduce a runtime tier either.
	unknown := emptyOutboundIdentitySettings()
	unknown.Runtime = map[string]map[string]string{"unknown": {kimi.HeaderDeviceName: "x"}}
	require.Error(t, svc.SetOutboundIdentitySettings(ctx, unknown))
}

func TestKimiAccountDeclarationsOverrideTheGlobalTier(t *testing.T) {
	config := emptyOutboundIdentitySettings()
	config.Runtime = map[string]map[string]string{"kimi": {
		kimi.HeaderDeviceName: "kimi-gateway-1",
		kimi.HeaderDeviceID:   "33333333-3333-4333-8333-333333333333",
	}}
	svc, ctx := outboundIdentityTestSettings(t, config)

	account := &Account{ID: 62, Platform: PlatformKimi, Type: AccountTypeAPIKey, Credentials: map[string]any{
		outboundIdentityCredential: OutboundIdentitySelection{Preset: "kimi", Headers: map[string]string{
			kimi.HeaderDeviceID: "44444444-4444-4444-8444-444444444444",
		}},
	}}
	got, ok := outboundidentity.FromContext(WithAccountOutboundIdentity(ctx, account))
	require.True(t, ok)
	require.Equal(t, "account", got.Source)
	require.Equal(t, "44444444-4444-4444-8444-444444444444", got.Headers[kimi.HeaderDeviceID])
	require.Equal(t, "kimi-gateway-1", got.Headers[kimi.HeaderDeviceName], "an undeclared override keeps the global value")

	// A complete account candidate still inherits the deployment-owned device
	// set rather than falling back to the compiled default.
	account.Credentials[outboundIdentityCredential] = OutboundIdentitySelection{Preset: "kimi", Version: "2.4.0", Headers: map[string]string{
		kimi.HeaderDeviceID: "55555555-5555-4555-8555-555555555555",
	}}
	got, ok = outboundidentity.FromContext(WithAccountOutboundIdentity(ctx, account))
	require.True(t, ok)
	require.Equal(t, "kimi-code-cli/2.4.0", got.UserAgent)
	require.Equal(t, "2.4.0", got.Headers[kimi.HeaderVersion])
	require.Equal(t, "kimi-gateway-1", got.Headers[kimi.HeaderDeviceName])
	require.Equal(t, "6.8.0-31-generic", got.Headers[kimi.HeaderOSVersion])

	preview, err := svc.PreviewOutboundIdentity(ctx, account, &OutboundIdentitySelection{Preset: "kimi", Version: "2.4.0", Headers: map[string]string{
		kimi.HeaderDeviceID: "55555555-5555-4555-8555-555555555555",
	}})
	require.NoError(t, err)
	require.Equal(t, got.Headers, preview.Headers, "the preview must report the declarations that reach the wire")

	// An invalid stored candidate falls through to the global tier instead of
	// failing the request.
	account.Credentials[outboundIdentityCredential] = OutboundIdentitySelection{Preset: "kimi", Headers: map[string]string{
		kimi.HeaderDeviceID: "not-a-uuid",
	}}
	got, ok = outboundidentity.FromContext(WithAccountOutboundIdentity(ctx, account))
	require.True(t, ok)
	require.Equal(t, "33333333-3333-4333-8333-333333333333", got.Headers[kimi.HeaderDeviceID], "an invalid account candidate falls through to the global tier")
}

// The settings page never needs a preset's header block in advance: the view
// reports the declared names, their class, and the built-in and effective
// values for every preset.
func TestOutboundIdentityDeclarationsCoverEveryBuiltInHeader(t *testing.T) {
	svc, ctx := outboundIdentityTestSettings(t, emptyOutboundIdentitySettings())
	view := svc.GetOutboundIdentityView(ctx)
	require.Len(t, view.Declarations, len(outboundPresetNames))

	for _, declarations := range view.Declarations {
		builtin := builtInOutboundIdentity(declarations.Preset)
		require.Len(t, declarations.Headers, len(builtin.Headers), declarations.Preset)
		seen := map[string]bool{}
		for _, header := range declarations.Headers {
			require.NotEmpty(t, header.Name, declarations.Preset)
			require.False(t, seen[header.Name], header.Name)
			seen[header.Name] = true
			require.Contains(t, builtin.Headers, header.Name, "the page must not claim an undeclared header")
			require.NotEmpty(t, header.Builtin, header.Name)
			require.Contains(t, []string{outboundHeaderDerived, outboundHeaderPinned, outboundHeaderRuntime}, header.Class, header.Name)
			require.Equal(t, header.Class == outboundHeaderRuntime, header.Editable, header.Name)
		}
	}
}

func TestOutboundIdentityDeclarationsListTheKimiBlockInOfficialOrder(t *testing.T) {
	declarations := declaredOutboundHeaders("kimi")
	names := make([]string, 0, len(declarations))
	for _, header := range declarations {
		names = append(names, header.Name)
	}
	require.Equal(t, []string{
		"User-Agent",
		kimi.HeaderPlatform,
		kimi.HeaderVersion,
		kimi.HeaderDeviceName,
		kimi.HeaderDeviceModel,
		kimi.HeaderOSVersion,
		kimi.HeaderDeviceID,
	}, names)
	require.Equal(t, outboundHeaderDerived, outboundHeaderClassName("kimi", kimi.HeaderVersion))
	require.Equal(t, outboundHeaderPinned, outboundHeaderClassName("kimi", kimi.HeaderPlatform))
	for _, name := range []string{kimi.HeaderDeviceName, kimi.HeaderDeviceID} {
		require.Equal(t, outboundHeaderRuntime, outboundHeaderClassName("kimi", name), name)
	}
	for _, name := range []string{kimi.HeaderDeviceModel, kimi.HeaderOSVersion} {
		require.Equal(t, outboundHeaderPinned, outboundHeaderClassName("kimi", name), name)
	}
	// Every other preset keeps its declarations read-only.
	require.Equal(t, outboundHeaderPinned, outboundHeaderClassName("claude", "X-App"))
	require.Equal(t, outboundHeaderPinned, outboundHeaderClassName("claude", "X-Stainless-Lang"))
	require.Equal(t, outboundHeaderDerived, outboundHeaderClassName("codex", "Version"))
	require.Equal(t, outboundHeaderDerived, outboundHeaderClassName("grok", "x-grok-client-version"))
}
