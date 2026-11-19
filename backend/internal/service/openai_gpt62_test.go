//go:build unit || !integration

package service

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/pkg/openai"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

// GPT-6.2 is advertised by the official catalog, so the gateway must recognize
// every spelling the client can allocate for it.
func TestGPT62ModelIDRecognizesPublishedSpellings(t *testing.T) {
	for _, model := range []string{
		"gpt-6.2-sol", "gpt-6.2-sol-high", "gpt-6.2-sol-openai-compact",
		"gpt-6.2-astra", "gpt-6.2-astra-max", "GPT-6.2-Astra",
	} {
		require.NotEmpty(t, openai.GPT62ModelID(model), model)
	}

	for _, model := range []string{
		"", "gpt-6.2", "gpt-6.2-solar", "gpt-6.1-sol", "gpt-6-sol", "claude-sonnet-4",
	} {
		require.Empty(t, openai.GPT62ModelID(model), model)
	}

	ids := openai.DefaultModelIDs()
	require.Contains(t, ids, "gpt-6.2-sol")
	require.Contains(t, ids, "gpt-6.2-astra")
}

// The published scale starts at low; disabled reasoning is rejected instead of
// being silently coerced.
func TestGPT62RejectsDisabledReasoning(t *testing.T) {
	for _, model := range []string{"gpt-6.2-sol", "gpt-6.2-astra"} {
		for _, effort := range []string{"none", "minimal", "NONE"} {
			require.Error(t, openai.ValidateGPT61ReasoningEffort(model, effort), model+" "+effort)
		}
		for _, effort := range []string{"low", "medium", "high", "xhigh", "max", ""} {
			require.NoError(t, openai.ValidateGPT61ReasoningEffort(model, effort), model+" "+effort)
		}
	}
}

// The Ultra Fast multiplier is resolved at the request instant: 3x before
// March 1, 2027 00:00 Pacific, 6x after, and 6x for every other model.
func TestOpenAIUltrafastMultiplierUsesRequestInstant(t *testing.T) {
	before := time.Date(2027, time.March, 1, 7, 59, 59, 0, time.UTC)
	after := before.Add(time.Second)

	for _, model := range []string{"gpt-6.2-sol", "gpt-6.2-astra"} {
		require.InDelta(t, 3, openAIUltrafastMultiplier(model, before), 1e-9, model)
		require.InDelta(t, 6, openAIUltrafastMultiplier(model, after), 1e-9, model)
	}
	require.InDelta(t, 6, openAIUltrafastMultiplier("gpt-6-astra", before), 1e-9)
	require.InDelta(t, 6, openAIUltrafastMultiplier("gpt-6.1-sol", before), 1e-9)
	// A zero instant resolves against the wall clock rather than the epoch.
	require.InDelta(t, openAIUltrafastMultiplier("gpt-6.2-sol", time.Now()), openAIUltrafastMultiplier("gpt-6.2-sol", time.Time{}), 1e-9)
}

func TestGPT62FallbackPricing(t *testing.T) {
	svc := newTestBillingService()

	astra := svc.getFallbackPricing("gpt-6.2-astra")
	require.NotNil(t, astra)
	require.InDelta(t, 8e-6, astra.InputPricePerToken, 1e-12)
	require.InDelta(t, 40e-6, astra.OutputPricePerToken, 1e-12)
	require.InDelta(t, 10e-6, astra.CacheCreationPricePerToken, 1e-12)
	require.InDelta(t, 0.4e-6, astra.CacheReadPricePerToken, 1e-12)
	require.True(t, astra.CacheCreationPriceExplicit)

	sol := svc.getFallbackPricing("gpt-6.2-sol")
	require.NotNil(t, sol)
	require.InDelta(t, 2e-6, sol.InputPricePerToken, 1e-12, "6.2 Sol shares the 6.1 Sol token rates")
	require.InDelta(t, 10e-6, sol.OutputPricePerToken, 1e-12)

	// The published name is reachable through the effort suffix as well.
	suffixed := svc.getFallbackPricing("gpt-6.2-sol-high")
	require.NotNil(t, suffixed)
	require.InDelta(t, 2e-6, suffixed.InputPricePerToken, 1e-12)
}

// GPT-6.2 offers the Ultra Fast tier, so group policy may force it and billing
// applies the request-instant multiplier.
func TestGPT62SupportsUltrafastTier(t *testing.T) {
	require.True(t, openAIModelSupportsUltrafast("gpt-6.2-sol"))
	require.True(t, openAIModelSupportsUltrafast("gpt-6.2-astra"))
	require.False(t, openAIModelSupportsUltrafast("gpt-5.6-sol"))
}

// Compat spellings collapse onto the published model ID so pricing and upstream
// routing agree on one name.
func TestGPT62CompatNormalization(t *testing.T) {
	require.Equal(t, "gpt-6.2-sol", NormalizeOpenAICompatRequestedModel("gpt-6.2-sol-high"))
	require.Equal(t, "gpt-6.2-astra", NormalizeOpenAICompatRequestedModel("GPT-6.2-Astra-openai-compact"))
	require.True(t, shouldAutoInjectPromptCacheKeyForCompat("gpt-6.2-sol"))
	require.True(t, shouldAutoInjectPromptCacheKeyForCompat("gpt-6.2-astra-high"))
}

// The catalog owns the base instructions; a missing entry would silently send the
// generic prompt to a model that expects its own.
func TestGPT62BaseInstructionsComeFromCatalog(t *testing.T) {
	for _, model := range []string{"gpt-6.2-sol", "gpt-6.2-astra"} {
		descriptor, ok := openai.CodexModelCatalogMetadata(model)
		require.True(t, ok, model)

		var metadata struct {
			ModelMessages struct {
				InstructionsTemplate string `json:"instructions_template"`
			} `json:"model_messages"`
		}
		require.NoError(t, json.Unmarshal(descriptor, &metadata), model)
		require.NotEmpty(t, metadata.ModelMessages.InstructionsTemplate, model)
		require.Equal(t, metadata.ModelMessages.InstructionsTemplate, openai.CodexBaseInstructionsForModel(model), model)
	}
}

// Pro plan entitlement fills the gap an older OAuth snapshot leaves when it only
// lists Fast: Pro 200 grants Sol, Pro 500 grants both, and the merged tier list
// keeps whatever the account already declared.
func TestGPT62PlanEntitlementMergesDeclaredTiers(t *testing.T) {
	oauthAccount := func(plan string) *Account {
		return &Account{ID: 1, Type: AccountTypeOAuth, Platform: PlatformOpenAI, Credentials: map[string]any{
			"plan_type": plan,
			"base_url":  "https://chatgpt.com/backend-api/codex",
		}}
	}

	pro := oauthAccount("Pro")
	require.True(t, gpt62PlanIncludesUltrafast(pro, "gpt-6.2-sol"))
	require.False(t, gpt62PlanIncludesUltrafast(pro, "gpt-6.2-astra"))

	proMax := oauthAccount("Pro-Max")
	require.True(t, gpt62PlanIncludesUltrafast(proMax, "gpt-6.2-sol"))
	require.True(t, gpt62PlanIncludesUltrafast(proMax, "gpt-6.2-astra"))

	plus := oauthAccount("Plus")
	require.False(t, gpt62PlanIncludesUltrafast(plus, "gpt-6.2-sol"))
	require.False(t, gpt62PlanIncludesUltrafast(plus, "gpt-6.2-astra"))

	require.False(t, gpt62PlanIncludesUltrafast(oauthAccount("Pro-Max"), "gpt-6.1-sol"), "only 6.2 is granted by plan")

	capabilities := accountCodexToolCapabilities(proMax, "gpt-6.2-astra")
	tiers := capabilities["service_tiers"]
	require.NotEmpty(t, tiers)
	require.True(t, gjson.GetBytes(tiers, `#(id=="ultrafast")`).Exists(), "Pro 500 grants Astra Ultrafast")
	require.True(t, gjson.GetBytes(tiers, `#(id=="priority")`).Exists(), "declared tiers are preserved")

	solOnly := accountCodexToolCapabilities(pro, "gpt-6.2-astra")
	require.False(t, gjson.GetBytes(solOnly["service_tiers"], `#(id=="ultrafast")`).Exists(), "Pro 200 does not grant Astra")
}
