package service

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/pkg/outboundidentity"
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

func TestOutboundIdentitySourcePriorityAndAccountTypes(t *testing.T) {
	for _, entry := range []struct{ platform, accountType, preset string }{
		{PlatformAnthropic, AccountTypeOAuth, "claude"}, {PlatformAnthropic, AccountTypeSetupToken, "claude"},
		{PlatformAnthropic, AccountTypeAPIKey, "claude"}, {PlatformAnthropic, AccountTypeBedrock, "claude"},
		{PlatformAnthropic, AccountTypeServiceAccount, "claude"}, {PlatformGemini, AccountTypeOAuth, "gemini"},
		{PlatformGemini, AccountTypeAPIKey, "gemini"}, {PlatformGemini, AccountTypeServiceAccount, "gemini"},
		{PlatformGrok, AccountTypeOAuth, "grok"}, {PlatformGrok, AccountTypeAPIKey, "grok"},
		{PlatformAntigravity, AccountTypeOAuth, "antigravity"}, {PlatformAntigravity, AccountTypeUpstream, "antigravity"},
		{PlatformKimi, AccountTypeAPIKey, "codex"}, {PlatformZhipu, AccountTypeAPIKey, "codex"},
		{PlatformDeepseek, AccountTypeAPIKey, "codex"}, {PlatformMiniMax, AccountTypeAPIKey, "codex"},
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
	for _, selection := range []OutboundIdentitySelection{{Preset: "unknown"}, {Preset: "claude", UserAgent: "claude-cli/3.9.1\r\nAuthorization: secret"}, {Preset: "gemini", UserAgent: strings.Repeat("x", 513)}, {Preset: "grok", Version: "invalid"}} {
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
	config.Defaults = map[string]string{"gemini:service_account": "grok"}
	require.NoError(t, svc.SetOutboundIdentitySettings(ctx, config))
	config.Defaults["gemini:service_account"] = "claude"
	view := svc.GetOutboundIdentitySettings(ctx)
	require.Equal(t, "grok", view.Defaults["gemini:service_account"])
	view.Defaults["gemini:service_account"] = "claude"
	require.Equal(t, "grok", svc.GetOutboundIdentitySettings(ctx).Defaults["gemini:service_account"])
	raw, err := svc.settingRepo.GetValue(ctx, SettingKeyOutboundIdentity)
	require.NoError(t, err)
	var persisted OutboundIdentitySettings
	require.NoError(t, json.Unmarshal([]byte(raw), &persisted))
	require.Equal(t, "grok", persisted.Defaults["gemini:service_account"])
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
