//go:build unit

package service

import (
	"net/http"
	"testing"

	"github.com/LuckyKuang/sub2api-plus/internal/pkg/outboundidentity"
	"github.com/stretchr/testify/require"
)

func TestStepFunIdentitySourcePriorityAndImmutableSDK(t *testing.T) {
	for _, kind := range []string{"oauth", "apikey"} {
		a := stepFunTestAccount(kind, "cn")
		_, ctx := outboundIdentityTestSettings(t, emptyOutboundIdentitySettings())
		identity, ok := outboundidentity.FromContext(WithAccountOutboundIdentity(ctx, a))
		require.True(t, ok)
		require.Equal(t, "stepfun", identity.Preset)
		require.Equal(t, "step", identity.Originator)
		require.Empty(t, identity.Version)
		require.Equal(t, "step (linux 6.8.0-31-generic; x64)", identity.UserAgent)
	}
	for _, candidate := range []OutboundIdentitySelection{
		{Preset: "stepfun", Version: "0.84.4"},
		{Preset: "stepfun", UserAgent: "step/0.84.4 (linux; node/v22.19.0; x64)"},
		{Preset: "stepfun", UserAgent: "step (darwin 24.0; arm64)"},
		{Preset: "stepfun", Headers: map[string]string{"X-Step-Client": "other"}},
		{Preset: "stepfun", Headers: map[string]string{"X-Stainless-Package-Version": "99.0.0"}},
		{Preset: "stepfun", Language: "en-US"}, {Preset: "stepfun", Timezone: "UTC"},
	} {
		_, err := buildOutboundIdentity(candidate)
		require.Error(t, err, "%+v", candidate)
	}
	settings := emptyOutboundIdentitySettings()
	settings.Defaults["stepfun:apikey"] = "claude"
	_, ctx := outboundIdentityTestSettings(t, settings)
	a := stepFunTestAccount("apikey", "cn")
	i, _ := outboundidentity.FromContext(WithAccountOutboundIdentity(ctx, a))
	require.Equal(t, "claude", i.Preset)
	a.Credentials[outboundIdentityCredential] = OutboundIdentitySelection{Preset: "stepfun"}
	i, _ = outboundidentity.FromContext(WithAccountOutboundIdentity(ctx, a))
	require.Equal(t, "stepfun", i.Preset)
	require.Equal(t, "account", i.Source)
	a.Credentials[outboundIdentityCredential] = OutboundIdentitySelection{Preset: "stepfun", Version: "1.0.0"}
	i, _ = outboundidentity.FromContext(WithAccountOutboundIdentity(ctx, a))
	require.Equal(t, "claude", i.Preset, "invalid account candidates fall through atomically")
	require.Error(t, NormalizeAccountOutboundIdentity("stepfun", "oauth", map[string]any{outboundIdentityCredential: OutboundIdentitySelection{Preset: "codex"}}))
}

func TestStepFunRetrySnapshotAndFailoverResolveCredentialOwner(t *testing.T) {
	settings, base := outboundIdentityTestSettings(t, emptyOutboundIdentitySettings())
	ctx := WithOutboundIdentityScope(base, nil)
	a := stepFunTestAccount("apikey", "cn")
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, a.GetOpenAIBaseURL()+"/chat/completions", nil)
	prepareAccountOutboundRequest(req, a)
	requireStepFunWire(t, req, true)
	updated := emptyOutboundIdentitySettings()
	updated.Defaults["stepfun:apikey"] = "grok"
	require.NoError(t, settings.SetOutboundIdentitySettings(ctx, updated))
	retry, _ := http.NewRequestWithContext(ctx, http.MethodPost, a.GetOpenAIBaseURL()+"/chat/completions", nil)
	prepareAccountOutboundRequest(retry, a)
	requireStepFunWire(t, retry, true)
	next := *a
	next.ID = 43
	failover, _ := http.NewRequestWithContext(ctx, http.MethodPost, next.GetOpenAIBaseURL()+"/chat/completions", nil)
	prepareAccountOutboundRequest(failover, &next)
	identity, ok := outboundidentity.FromContext(failover.Context())
	require.True(t, ok)
	require.Equal(t, "grok", identity.Preset)
	require.Empty(t, failover.Header.Get("X-Step-Client"))
	require.Empty(t, failover.Header.Get("X-Stainless-Package-Version"))
}

func TestStepFunPlatformCapabilitiesAndCredentialValidation(t *testing.T) {
	require.Contains(t, AllowedQuotaPlatforms, "stepfun")
	require.Equal(t, "stepfun", NormalizeOpenAICompatiblePlatform("stepfun"))
	for _, model := range []string{"step-3.7-flash", "stepfun/future-model", "step/future-model"} {
		platform, ok := DetectModelPlatform(model)
		require.True(t, ok)
		require.Equal(t, "stepfun", platform)
	}
	require.True(t, providerSupportsProbe("stepfun"))
	require.ErrorIs(t, monitorAccountQuotaCapability(stepFunTestAccount("oauth", "cn")), ErrChannelMonitorAccountNotSupportable)
	for _, kind := range []string{"apikey", "oauth"} {
		a := stepFunTestAccount(kind, "global")
		require.NoError(t, validateStepFunCredentials(a.Platform, a.Type, a.Credentials))
		if kind == "oauth" {
			a.Credentials["account_mode"] = AccountModePayG
			require.Error(t, validateStepFunCredentials(a.Platform, a.Type, a.Credentials), "Step Plan grants must not be saved as pay-as-you-go accounts")
			a.Credentials["account_mode"] = AccountModeCoding
		}
		a.Credentials["api_protocol"] = "anthropic"
		require.Error(t, validateStepFunCredentials(a.Platform, a.Type, a.Credentials))
		a.Credentials["api_protocol"] = "chat_completions"
		a.Credentials["region"] = "invalid"
		a.Credentials["oauth_region"] = "invalid"
		require.Error(t, validateStepFunCredentials(a.Platform, a.Type, a.Credentials))
	}
}
