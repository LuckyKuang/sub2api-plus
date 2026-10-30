//go:build unit

package service

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestPlatformPricingFallback_CompiledClaudeSpellingsKeepTheirOwnSKU(t *testing.T) {
	for _, model := range []string{
		"claude-opus-4-5", "claude-opus-4-5-20251101", "claude-opus-4-6",
		"anthropic/claude-opus-4-6", "anthropic/claude-opus-4.6", "claude-opus-4-7",
	} {
		t.Run(model, func(t *testing.T) {
			billing := platformFallbackTestBilling(nil)
			billing.frozenPricingFallbackModels = map[string]string{PlatformAnthropic: ""}
			price, err := billing.GetModelPricingForPlatform(context.Background(), PlatformAnthropic, model)
			require.NoError(t, err)
			requirePlatformFallbackRates(t, price, 5e-6, 25e-6)
			require.Empty(t, price.FallbackModel)
		})
	}
	// Sonnet 4.6 has a catalog SKU, but no independent compiled card. Having
	// Sonnet 4 in the compiled map is not permission to borrow another SKU.
	billing := platformFallbackTestBilling(nil)
	billing.frozenPricingFallbackModels = map[string]string{PlatformAnthropic: ""}
	for _, model := range []string{"claude-sonnet-4-6", "anthropic/claude-sonnet-4.6"} {
		price, err := billing.GetModelPricingForPlatform(context.Background(), PlatformAnthropic, model)
		require.ErrorIs(t, err, ErrModelPricingUnavailable)
		require.Nil(t, price)
	}
}

func TestPlatformPricingFallback_DynamicClaudeSpellingsAndRegisteredSnapshots(t *testing.T) {
	for _, tc := range []struct {
		key, request  string
		input, output float64
	}{
		{"claude-opus-4.6", "claude-opus-4-6", 5e-6, 25e-6},
		{"claude-opus-4-6", "anthropic/claude-opus-4.6", 5e-6, 25e-6},
		{"claude-sonnet-4.6", "anthropic/claude-sonnet-4-6", 3e-6, 15e-6},
		{"claude-sonnet-4-6", "claude-sonnet-4.6", 3e-6, 15e-6},
		{"claude-opus-4.5", "anthropic/claude-opus-4-5-20251101", 5e-6, 25e-6},
		{"claude-sonnet-4-5-20250929", "claude-sonnet-4.5", 3e-6, 15e-6},
	} {
		t.Run(tc.request+"/"+tc.key, func(t *testing.T) {
			entry := &LiteLLMModelPricing{InputCostPerToken: tc.input, OutputCostPerToken: tc.output}
			catalog := &PricingService{pricingData: map[string]*LiteLLMModelPricing{tc.key: entry}}
			require.Same(t, entry, catalog.GetIdentifiedModelPricing(tc.request))
			billing := platformFallbackTestBilling(catalog)
			billing.frozenPricingFallbackModels = map[string]string{PlatformAnthropic: ""}
			price, err := billing.GetModelPricingForPlatform(context.Background(), PlatformAnthropic, tc.request)
			require.NoError(t, err)
			requirePlatformFallbackRates(t, price, tc.input, tc.output)
			require.Empty(t, price.FallbackModel)
		})
	}
}

func TestPlatformPricingFallback_RegisteredDeepSeekAliasesPreserveFlashAndProRates(t *testing.T) {
	for _, tc := range []struct {
		model                           string
		input, output, cacheRead, total float64
	}{
		{"deepseek-flash", 0.15e-6, 0.60e-6, 0.003e-6, 0.000453},
		{"deepseek-v4.1-flash", 0.15e-6, 0.60e-6, 0.003e-6, 0.000453},
		{"deepseek-v4-flash-0731", 0.15e-6, 0.60e-6, 0.003e-6, 0.000453},
		{"deepseek/deepseek-v4-flash-0731", 0.15e-6, 0.60e-6, 0.003e-6, 0.000453},
		{"deepseek-v4-pro-0813", 0.66e-6, 1.98e-6, 0.022e-6, 0.001672},
	} {
		t.Run(tc.model, func(t *testing.T) {
			billing := platformFallbackTestBilling(nil)
			billing.frozenPricingFallbackModels = map[string]string{PlatformDeepseek: ""}
			price, err := billing.GetModelPricingForPlatform(context.Background(), PlatformDeepseek, tc.model)
			require.NoError(t, err)
			requirePlatformFallbackRates(t, price, tc.input, tc.output)
			require.InDelta(t, tc.cacheRead, price.CacheReadPricePerToken, 1e-15)
			require.Empty(t, price.FallbackModel)
			cost, err := billing.CalculateCostUnified(CostInput{
				Ctx: context.Background(), Model: tc.model,
				Group: &Group{ID: 77, Platform: PlatformDeepseek}, RateMultiplier: 1,
				// These alias rates describe Sunday off-peak pricing. Pin the
				// request time too; zero intentionally uses the current time.
				PricingAt: time.Date(2026, 10, 25, 12, 0, 0, 0, time.UTC),
				Tokens:    UsageTokens{InputTokens: 1000, OutputTokens: 500, CacheReadTokens: 1000},
				Resolver:  NewModelPricingResolver(nil, billing),
			})
			require.NoError(t, err)
			require.InDelta(t, tc.total, cost.TotalCost, 1e-12)
		})
	}
}

func TestPlatformPricingFallback_DatedGPT54RetainsDynamicLongContextCard(t *testing.T) {
	entry := &LiteLLMModelPricing{
		InputCostPerToken: 2.5e-6, OutputCostPerToken: 15e-6,
		LongContextInputTokenThreshold: 272000,
		LongContextInputCostMultiplier: 2, LongContextOutputCostMultiplier: 1.5,
	}
	catalog := &PricingService{pricingData: map[string]*LiteLLMModelPricing{"gpt-5.4": entry}}
	billing := platformFallbackTestBilling(catalog)
	billing.frozenPricingFallbackModels = map[string]string{PlatformOpenAI: ""}
	for _, model := range []string{"gpt-5.4-2026-03-05", "openai/gpt-5.4-2026-03-05", "gpt-5.4-20260305"} {
		t.Run(model, func(t *testing.T) {
			require.Same(t, entry, catalog.GetIdentifiedModelPricing(model))
			price, err := billing.GetModelPricingForPlatform(context.Background(), PlatformOpenAI, model)
			require.NoError(t, err)
			requirePlatformFallbackRates(t, price, 2.5e-6, 15e-6)
			require.Equal(t, 272000, price.LongContextInputThreshold)
			require.Equal(t, float64(2), price.LongContextInputMultiplier)
			require.Equal(t, 1.5, price.LongContextOutputMultiplier)
			require.Empty(t, price.FallbackModel)
			cost, err := billing.CalculateCostUnified(CostInput{
				Ctx: context.Background(), Model: model,
				Group:  &Group{ID: 77, Platform: PlatformOpenAI, LongContextPricingEnabled: true},
				Tokens: UsageTokens{InputTokens: 300000, OutputTokens: 2000}, RateMultiplier: 1,
				Resolver: NewModelPricingResolver(nil, billing),
			})
			require.NoError(t, err)
			require.InDelta(t, 1.545, cost.TotalCost, 1e-12)
		})
	}
}

func TestPlatformPricingFallback_ExplicitAliasCardsPrecedeNormalizedBase(t *testing.T) {
	for _, tc := range []struct {
		platform, model, base string
	}{
		{PlatformAnthropic, "anthropic/claude-opus-4-6", "claude-opus-4.6"},
		{PlatformAnthropic, "claude-sonnet-5-thinking", "claude-sonnet-5"},
		{PlatformOpenAI, "gpt-5.4-2026-03-05", "gpt-5.4"},
	} {
		t.Run(tc.model, func(t *testing.T) {
			exact := &LiteLLMModelPricing{InputCostPerToken: 7e-6, OutputCostPerToken: 11e-6}
			catalog := &PricingService{pricingData: map[string]*LiteLLMModelPricing{
				tc.model: exact, tc.base: {InputCostPerToken: 1e-6, OutputCostPerToken: 2e-6},
			}}
			require.Same(t, exact, catalog.GetIdentifiedModelPricing(tc.model))
			billing := platformFallbackTestBilling(catalog)
			billing.frozenPricingFallbackModels = map[string]string{tc.platform: ""}
			price, err := billing.GetModelPricingForPlatform(context.Background(), tc.platform, tc.model)
			require.NoError(t, err)
			requirePlatformFallbackRates(t, price, 7e-6, 11e-6)
			require.Empty(t, price.FallbackModel)
		})
	}
}

func TestPlatformPricingFallback_UnknownSKUDoesNotAcquireAnAliasPrice(t *testing.T) {
	catalog := &PricingService{pricingData: map[string]*LiteLLMModelPricing{
		"claude-opus-4.6":   {InputCostPerToken: 5e-6, OutputCostPerToken: 25e-6},
		"gpt-5.4":           {InputCostPerToken: 2.5e-6, OutputCostPerToken: 15e-6},
		"gpt-6.1-terra":     {InputCostPerToken: 1e-6, OutputCostPerToken: 5e-6},
		"gpt-6-astra":       {InputCostPerToken: 50e-6, OutputCostPerToken: 200e-6},
		"deepseek-v4-flash": {InputCostPerToken: 0.15e-6, OutputCostPerToken: 0.60e-6},
		"step-3.5-flash":    {InputCostPerToken: 0.1e-6, OutputCostPerToken: 0.3e-6},
	}}
	for _, tc := range []struct{ platform, model string }{
		{PlatformAnthropic, "claude-opus-4-6-preview"},
		{PlatformAnthropic, "anthropic/claude-opus-future"},
		{PlatformOpenAI, "gpt-5.4-preview"},
		{PlatformOpenAI, "gpt-5.4-codex-unpublished"},
		{PlatformOpenAI, "gpt-6.1-terra-preview"},
		{PlatformOpenAI, "gpt-6.1-terra-2026-10-28"},
		{PlatformOpenAI, "gpt-6.1-terra-20261028"},
		{PlatformOpenAI, "gpt-6-astra-2026-10-28"},
		{PlatformOpenAI, "gpt-6-astra-20261028"},
		{PlatformOpenAI, "openai/gpt-6-astra-2026-10-28"},
		{PlatformOpenAI, "gpt_6_astra_20261028"},
		{PlatformDeepseek, "deepseek-v4-flash-preview"},
		{PlatformDeepseek, "deepseek-v4-flash-20261028"},
		{PlatformDeepseek, "deepseek-v4-pro-unknown"},
		{PlatformStepFun, "step-3.5-flash-2026-10-28"},
		{PlatformStepFun, "step-3.5-flash-20261028"},
	} {
		t.Run(tc.model, func(t *testing.T) {
			require.Nil(t, catalog.GetIdentifiedModelPricing(tc.model))
			for _, source := range []*PricingService{nil, catalog} {
				billing := platformFallbackTestBilling(source)
				billing.frozenPricingFallbackModels = map[string]string{tc.platform: ""}
				price, err := billing.GetModelPricingForPlatform(context.Background(), tc.platform, tc.model)
				require.ErrorIs(t, err, ErrModelPricingUnavailable)
				require.Nil(t, price)
			}
		})
	}
	// An exact configured price for a distinct SKU remains authoritative.
	for _, model := range []string{"gpt-6.1-terra-preview", "gpt-5.4-preview", "gpt-6-astra-2026-10-28", "step-3.5-flash-20261028"} {
		entry := &LiteLLMModelPricing{InputCostPerToken: 7e-6, OutputCostPerToken: 11e-6}
		catalog.pricingData[model] = entry
		require.Same(t, entry, catalog.GetIdentifiedModelPricing(model))
	}
}

func TestPlatformPricingFallback_AstraCanonicalCannotBorrowUnregisteredSnapshotPrices(t *testing.T) {
	// Astra's contract registers only the canonical SKU. An independently
	// priced snapshot does not establish the canonical model's dynamic price.
	catalog := &PricingService{pricingData: map[string]*LiteLLMModelPricing{
		"gpt-6-astra-2026-10-28": {InputCostPerToken: 7e-6, OutputCostPerToken: 11e-6},
		"gpt-6-astra-20261029":   {InputCostPerToken: 9e-6, OutputCostPerToken: 13e-6},
	}}
	require.Nil(t, catalog.GetIdentifiedModelPricing("gpt-6-astra"))
	require.Nil(t, catalog.GetIdentifiedModelPricing("openai/gpt-6-astra"))
	price := catalog.GetIdentifiedModelPricing("gpt-6-astra-2026-10-28")
	require.NotNil(t, price)
	require.Equal(t, 7e-6, price.InputCostPerToken)
	require.Equal(t, 11e-6, price.OutputCostPerToken)
}
