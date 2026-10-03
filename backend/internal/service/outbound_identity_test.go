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
		{PlatformKimi, AccountTypeAPIKey, "codex"}, {PlatformZhipu, AccountTypeAPIKey, "zcode"},
		{PlatformZhipu, AccountTypeOAuth, "zcode"},
		{PlatformDeepseek, AccountTypeAPIKey, "deepseek"}, {PlatformMiniMax, AccountTypeAPIKey, "minimax"},
	} {
		t.Run(entry.platform+"/"+entry.accountType, func(t *testing.T) {
			account := &Account{ID: 42, Platform: entry.platform, Type: entry.accountType, Credentials: map[string]any{}}
			_, ctx := outboundIdentityTestSettings(t, emptyOutboundIdentitySettings())
			got, ok := outboundidentity.FromContext(WithAccountOutboundIdentity(ctx, account))
			require.True(t, ok)
			require.Equal(t, entry.preset, got.Preset)
			require.Equal(t, builtInOutboundIdentity(entry.preset).UserAgent, got.UserAgent)
			if entry.preset == "codex" {
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
	config.Defaults["gemini:service_account"] = "claude"
	svc, ctx := outboundIdentityTestSettings(t, config)
	a := &Account{ID: 1, Platform: PlatformGemini, Type: AccountTypeServiceAccount, Credentials: map[string]any{outboundIdentityCredential: OutboundIdentitySelection{Preset: "grok"}}}
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
	b := &Account{ID: 2, Platform: PlatformGemini, Type: AccountTypeServiceAccount}
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
		require.Equal(t, strings.Replace(before.UserAgent, "/"+before.Version, "/3.9.1", 1), after.UserAgent)
		for key, value := range before.Headers {
			if key != "User-Agent" && key != "Version" && key != "x-grok-client-version" {
				require.Equal(t, value, after.Headers[key], key)
			}
		}
	}
	for _, selection := range []OutboundIdentitySelection{{Preset: "unknown"}, {Preset: "claude", UserAgent: "claude-cli/3.9.1\r\nAuthorization: secret"}, {Preset: "gemini", UserAgent: strings.Repeat("x", 513)}, {Preset: "grok", Version: "invalid"}, {Preset: "deepseek", UserAgent: "deepseek/0.2.0-rc.2"}, {Preset: "deepseek", UserAgent: "deepseek-harness/0.2.0-rc.2 (sub2api)"}, {Preset: "deepseek", Version: "0.0.1"}, {Preset: "minimax", UserAgent: "minimax/0.6.2"}, {Preset: "minimax", UserAgent: "MiniMaxAgent/0.6.2"}, {Preset: "minimax", UserAgent: "MiniMaxAgent (sub2api)"}, {Preset: "minimax", Version: "0.6.2"}, {Preset: "zcode", UserAgent: "zcode/3.14.3"}, {Preset: "zcode", UserAgent: "ZCode"}, {Preset: "zcode", UserAgent: "ZCode/3.14"}, {Preset: "zcode", UserAgent: "ZCode/3.14.3 (sub2api)"}, {Preset: "zcode", Version: "invalid"}} {
		_, err := buildOutboundIdentity(selection)
		require.Error(t, err)
	}
	credentials := map[string]any{outboundIdentityCredential: OutboundIdentitySelection{Preset: "grok"}}
	require.Error(t, NormalizeAccountOutboundIdentity(PlatformGemini, AccountTypeOAuth, credentials))
	require.NoError(t, NormalizeAccountOutboundIdentity(PlatformGemini, AccountTypeServiceAccount, credentials))
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
	config.Defaults = map[string]string{"gemini:service_account": " grok "}
	require.NoError(t, svc.SetOutboundIdentitySettings(ctx, config))
	config.Defaults["gemini:service_account"] = "claude"
	view := svc.GetOutboundIdentitySettings(ctx)
	require.Equal(t, "grok", view.Defaults["gemini:service_account"])
	require.Equal(t, OutboundIdentitySelection{Preset: "grok", Version: "3.9.1"}, view.Profiles["grok"])
	view.Defaults["gemini:service_account"] = "claude"
	require.Equal(t, "grok", svc.GetOutboundIdentitySettings(ctx).Defaults["gemini:service_account"])
	raw, err := svc.settingRepo.GetValue(ctx, SettingKeyOutboundIdentity)
	require.NoError(t, err)
	var persisted OutboundIdentitySettings
	require.NoError(t, json.Unmarshal([]byte(raw), &persisted))
	require.Equal(t, "grok", persisted.Defaults["gemini:service_account"])
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
			require.Equal(t, builtInOutboundIdentity(preset).UserAgent, req.Header.Get("User-Agent"))
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
// This test is the audit record for that default, for the equivalent explicit
// `deepseek:apikey` type default, and for the per-account opt-out.
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

	// The explicit type default is an equivalent, operator-visible pin.
	config := emptyOutboundIdentitySettings()
	config.Defaults["deepseek:apikey"] = "deepseek"
	_, pinnedCtx := outboundIdentityTestSettings(t, config)
	got, ok = outboundidentity.FromContext(WithAccountOutboundIdentity(pinnedCtx, account))
	require.True(t, ok)
	require.Equal(t, "deepseek", got.Preset)
	require.Equal(t, "compiled_default", got.Source)

	// An account selection can still opt back into another compatible preset.
	override := &Account{ID: 10, Platform: PlatformDeepseek, Type: AccountTypeAPIKey, Credentials: map[string]any{
		outboundIdentityCredential: OutboundIdentitySelection{Preset: "codex"},
	}}
	got, ok = outboundidentity.FromContext(WithAccountOutboundIdentity(ctx, override))
	require.True(t, ok)
	require.Equal(t, "codex", got.Preset)
	require.Equal(t, "account", got.Source)
}

// The MiniMax preset pins the published MiniMax Code product declaration. The
// official client family renders the bare token `MiniMaxAgent` and never puts a
// version segment on the wire, so the exact string is asserted rather than a
// versioned prefix, and Version stays intentionally empty.
func TestBuiltInMiniMaxOutboundIdentityPinsProductToken(t *testing.T) {
	const pinnedUA = "MiniMaxAgent"
	require.Equal(t, outboundidentity.Identity{
		Preset:     "minimax",
		Source:     "compiled_default",
		UserAgent:  pinnedUA,
		Originator: pinnedUA,
		Version:    "",
		Headers:    map[string]string{"User-Agent": pinnedUA},
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
	require.Equal(t, map[string]string{"minimax": minimax.ProductToken}, versionlessOutboundUserAgents)
	for _, preset := range outboundPresetNames {
		require.Equal(t, preset == "minimax", versionlessOutboundUserAgents[preset] != "", preset)
	}

	credentials := map[string]any{outboundIdentityCredential: OutboundIdentitySelection{Preset: "minimax"}}
	require.NoError(t, NormalizeAccountOutboundIdentity(PlatformMiniMax, AccountTypeAPIKey, credentials))
	require.Equal(t, OutboundIdentitySelection{Preset: "minimax"}, credentials[outboundIdentityCredential])
}

// A MiniMax account that selects the preset must render only the User-Agent
// declaration. Codex's Originator/Version stay off the wire, while protocol
// request state such as the MiniMax session headers keeps its own ownership.
func TestMiniMaxOutboundIdentityRendersOnlyUserAgent(t *testing.T) {
	_, ctx := outboundIdentityTestSettings(t, emptyOutboundIdentitySettings())
	account := &Account{ID: 8, Platform: PlatformMiniMax, Type: AccountTypeAPIKey, Credentials: map[string]any{
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
	require.Equal(t, "request-state", req.Header.Get("X-Mavis-Session-Id"), "request state is not an identity declaration")
	require.Equal(t, "2023-06-01", req.Header.Get("Anthropic-Version"), "protocol versions are not identity declarations")
}

// MiniMax platform accounts advertise the pinned product identity by default.
// This test is the audit record for that default, for the equivalent explicit
// `minimax:apikey` type default, and for the per-account opt-out.
func TestMiniMaxDefaultIdentityIsPinnedProduct(t *testing.T) {
	require.Equal(t, "minimax", nativeOutboundPreset(PlatformMiniMax))

	account := &Account{ID: 11, Platform: PlatformMiniMax, Type: AccountTypeAPIKey, Credentials: map[string]any{}}
	_, ctx := outboundIdentityTestSettings(t, emptyOutboundIdentitySettings())
	got, ok := outboundidentity.FromContext(WithAccountOutboundIdentity(ctx, account))
	require.True(t, ok)
	require.Equal(t, "minimax", got.Preset)
	require.Equal(t, "compiled_default", got.Source)
	require.Equal(t, minimax.ProductToken, got.UserAgent)
	require.Equal(t, minimax.ProductToken, got.Originator)
	require.Empty(t, got.Version)
	require.Equal(t, map[string]string{"User-Agent": got.UserAgent}, got.Headers)

	// The explicit type default is an equivalent, operator-visible pin.
	config := emptyOutboundIdentitySettings()
	config.Defaults["minimax:apikey"] = "minimax"
	_, pinnedCtx := outboundIdentityTestSettings(t, config)
	got, ok = outboundidentity.FromContext(WithAccountOutboundIdentity(pinnedCtx, account))
	require.True(t, ok)
	require.Equal(t, "minimax", got.Preset)
	require.Equal(t, "compiled_default", got.Source)

	// An account selection can still opt back into another compatible preset.
	override := &Account{ID: 12, Platform: PlatformMiniMax, Type: AccountTypeAPIKey, Credentials: map[string]any{
		outboundIdentityCredential: OutboundIdentitySelection{Preset: "codex"},
	}}
	got, ok = outboundidentity.FromContext(WithAccountOutboundIdentity(ctx, override))
	require.True(t, ok)
	require.Equal(t, "codex", got.Preset)
	require.Equal(t, "account", got.Source)
}

func TestBuiltInZCodeOutboundIdentityPinsProductVersion(t *testing.T) {
	t.Setenv(zcode.VersionEnv, "")

	const pinnedUA = "ZCode/3.14.3"
	require.Equal(t, outboundidentity.Identity{
		Preset:     "zcode",
		Source:     "compiled_default",
		UserAgent:  pinnedUA,
		Originator: "ZCode",
		Version:    "3.14.3",
		Headers:    map[string]string{"User-Agent": pinnedUA},
	}, builtInOutboundIdentity("zcode"))
	require.Equal(t, builtInOutboundIdentity("zcode"), zcode.DefaultIdentity())
	require.Equal(t, pinnedUA, zcode.UserAgent(zcode.DefaultVersion))
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
		require.Equal(t, map[string]string{"User-Agent": configured.UserAgent}, configured.Headers)
	}

	for _, invalid := range []string{"", "not-a-version", "3.14", "3.14.3.1", "3.14.3 x"} {
		t.Setenv(zcode.VersionEnv, invalid)
		fallback := builtInOutboundIdentity("zcode")
		require.Equal(t, "compiled_default", fallback.Source, invalid)
		require.Equal(t, zcode.DefaultVersion, fallback.Version, invalid)
	}
}

// A Zhipu account that selects the preset must render only the User-Agent
// declaration, matching the official client which declares no Originator and no
// standalone version header. Codex's declarations stay off the wire, while
// protocol request state keeps its own ownership.
func TestZCodeOutboundIdentityRendersOnlyUserAgent(t *testing.T) {
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

	require.Equal(t, zcode.UserAgent(zcode.DefaultVersion), req.Header.Get("User-Agent"))
	require.Empty(t, req.Header.Get("Originator"))
	require.Empty(t, req.Header.Get("Version"))
	require.Equal(t, "request-state", req.Header.Get("X-ZCode-App-Version"), "platform attribution is not an identity declaration")
	require.Equal(t, "2023-06-01", req.Header.Get("Anthropic-Version"), "protocol versions are not identity declarations")
}

// Zhipu / GLM platform accounts advertise the pinned ZCode identity by default,
// for both API-key and account-link OAuth accounts. This test is the audit record
// for that default, for the equivalent explicit `zhipu:apikey` type default, and
// for the per-account opt-out.
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
		require.Equal(t, map[string]string{"User-Agent": got.UserAgent}, got.Headers, accountType)
	}

	// The explicit type default is an equivalent, operator-visible pin.
	account := &Account{ID: 15, Platform: PlatformZhipu, Type: AccountTypeAPIKey, Credentials: map[string]any{}}
	config := emptyOutboundIdentitySettings()
	config.Defaults["zhipu:apikey"] = "zcode"
	_, pinnedCtx := outboundIdentityTestSettings(t, config)
	got, ok := outboundidentity.FromContext(WithAccountOutboundIdentity(pinnedCtx, account))
	require.True(t, ok)
	require.Equal(t, "zcode", got.Preset)
	require.Equal(t, "compiled_default", got.Source)

	// An API-key account selection can still opt back into another compatible
	// preset.
	_, ctx := outboundIdentityTestSettings(t, emptyOutboundIdentitySettings())
	override := &Account{ID: 16, Platform: PlatformZhipu, Type: AccountTypeAPIKey, Credentials: map[string]any{
		outboundIdentityCredential: OutboundIdentitySelection{Preset: "codex"},
	}}
	got, ok = outboundidentity.FromContext(WithAccountOutboundIdentity(ctx, override))
	require.True(t, ok)
	require.Equal(t, "codex", got.Preset)
	require.Equal(t, "account", got.Source)

	// An OAuth account is pinned to its native family and cannot opt out; the
	// selection is rejected and the account keeps the ZCode identity.
	oauthOverride := &Account{ID: 17, Platform: PlatformZhipu, Type: AccountTypeOAuth, Credentials: map[string]any{
		outboundIdentityCredential: OutboundIdentitySelection{Preset: "codex"},
	}}
	got, ok = outboundidentity.FromContext(WithAccountOutboundIdentity(ctx, oauthOverride))
	require.True(t, ok)
	require.Equal(t, "zcode", got.Preset, "OAuth accounts must retain their native client family")
}
