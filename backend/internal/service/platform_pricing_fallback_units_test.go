//go:build unit

package service

import (
	"context"
	"math"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPlatformPricingFallback_ParsedTokenDimensionPresenceDistinguishesMissingAndFree(t *testing.T) {
	catalog := newStubPricingServiceFromJSON(t, `{
		"input-only": {"input_cost_per_token":0.000001},
		"output-only": {"output_cost_per_token":0.000002},
		"image-only": {"mode":"image_generation","output_cost_per_image":0.02},
		"image-token-mixed": {"mode":"image_generation","input_cost_per_token":0.000005,"output_cost_per_image_token":0.00003},
		"text-free": {"input_cost_per_token":0,"output_cost_per_token":0},
		"text-paid": {"input_cost_per_token":0.000001,"output_cost_per_token":0.000002}
	}`)
	for _, tc := range []struct {
		model                                string
		inputAbsent, outputAbsent, allAbsent bool
	}{
		{"input-only", false, true, false},
		{"output-only", true, false, false},
		{"image-only", true, true, true},
		{"image-token-mixed", false, true, false},
		{"text-free", false, false, false},
		{"text-paid", false, false, false},
	} {
		t.Run(tc.model, func(t *testing.T) {
			entry := catalog.GetIdentifiedModelPricing(tc.model)
			require.NotNil(t, entry)
			require.Equal(t, tc.inputAbsent, entry.InputTokenPriceAbsent)
			require.Equal(t, tc.outputAbsent, entry.OutputTokenPriceAbsent)
			require.Equal(t, tc.allAbsent, entry.TokenPricingAbsent)
		})
	}
}

func TestPlatformPricingFallback_RejectsIncompleteOrIncompatibleFinalCards(t *testing.T) {
	catalog := newStubPricingServiceFromJSON(t, `{
		"input-only": {"input_cost_per_token":0.000001},
		"output-only": {"output_cost_per_token":0.000002},
		"image-only": {"mode":"image_generation","output_cost_per_image":0.02},
		"image-token-mixed": {"mode":"image_generation","input_cost_per_token":0.000005,"output_cost_per_image_token":0.00003},
		"negative-input": {"input_cost_per_token":-0.000001,"output_cost_per_token":0.000002},
		"negative-cache": {"input_cost_per_token":0.000001,"output_cost_per_token":0.000002,"cache_read_input_token_cost":-0.000001}
	}`)
	for _, target := range []string{"input-only", "output-only", "image-only", "image-token-mixed", "negative-input", "negative-cache"} {
		t.Run(target, func(t *testing.T) {
			billing := platformFallbackTestBilling(catalog)
			billing.frozenPricingFallbackModels = map[string]string{PlatformOpenAI: target}
			price, err := billing.GetModelPricingForPlatform(context.Background(), PlatformOpenAI, "unpublished-request-sku")
			require.ErrorIs(t, err, ErrModelPricingUnavailable)
			require.Nil(t, price)
			group := &Group{ID: 77, Platform: PlatformOpenAI, RateMultiplier: 1}
			resolver := NewModelPricingResolver(nil, billing)
			cost, err := billing.CalculateTokenCostForRequest(TokenCostRequest{
				Ctx: context.Background(), Group: group, Model: "unpublished-request-sku", RateMultiplier: 1,
				Tokens: UsageTokens{InputTokens: 1000, OutputTokens: 1000}, Resolver: resolver,
			})
			require.ErrorIs(t, err, ErrModelPricingUnavailable)
			require.Nil(t, cost, "a missing dimension must not become a partially free successful charge")
		})
	}
}

func TestPlatformPricingFallback_CompleteSameSKUCompiledCardFillsOnlyMissingDynamicDimensions(t *testing.T) {
	catalog := newStubPricingServiceFromJSON(t, `{
		"gpt-5.4-mini": {"litellm_provider":"openai","input_cost_per_token":0.000099},
		"gpt-5.4-nano": {"litellm_provider":"openai","output_cost_per_token":0},
		"gpt-5.4": {"litellm_provider":"openai","mode":"image_generation","output_cost_per_image":0.02}
	}`)
	for _, tc := range []struct {
		model         string
		input, output float64
	}{
		{"gpt-5.4-mini", 99e-6, 4.5e-6},
		{"gpt-5.4-nano", 0.2e-6, 0},
		{"gpt-5.4", 2.5e-6, 15e-6},
	} {
		t.Run(tc.model, func(t *testing.T) {
			billing := platformFallbackTestBilling(catalog)
			billing.frozenPricingFallbackModels = map[string]string{PlatformOpenAI: tc.model}
			price, err := billing.GetModelPricingForPlatform(context.Background(), PlatformOpenAI, "unpublished-request-sku")
			require.NoError(t, err)
			requirePlatformFallbackRates(t, price, tc.input, tc.output)
			require.Equal(t, tc.model, price.FallbackModel)
		})
	}
}

func TestPlatformPricingFallback_ExplicitTextZeroAndJevRemainValid(t *testing.T) {
	catalog := newStubPricingServiceFromJSON(t, `{
		"explicit-free": {"input_cost_per_token":0,"output_cost_per_token":0},
		"explicit-output-free": {"input_cost_per_token":0.000001,"output_cost_per_token":0}
	}`)
	for _, tc := range []struct {
		model               string
		input, output, cost float64
	}{
		{"explicit-free", 0, 0, 0},
		{"explicit-output-free", 1e-6, 0, 0.001},
		{"jev-latest", 0.042e-6, 0, 0.000042},
	} {
		t.Run(tc.model, func(t *testing.T) {
			billing := platformFallbackTestBilling(catalog)
			billing.frozenPricingFallbackModels = map[string]string{PlatformOpenAI: tc.model}
			price, err := billing.GetModelPricingForPlatform(context.Background(), PlatformOpenAI, "unpublished-request-sku")
			require.NoError(t, err)
			requirePlatformFallbackRates(t, price, tc.input, tc.output)
			group := &Group{ID: 77, Platform: PlatformOpenAI, RateMultiplier: 1}
			resolver := NewModelPricingResolver(nil, billing)
			cost, err := billing.CalculateTokenCostForRequest(TokenCostRequest{
				Ctx: context.Background(), Group: group, Model: "unpublished-request-sku", RateMultiplier: 1,
				Tokens: UsageTokens{InputTokens: 1000, OutputTokens: 1000}, Resolver: resolver,
			})
			require.NoError(t, err)
			require.InDelta(t, tc.cost, cost.TotalCost, 1e-12)
			require.InDelta(t, tc.cost, cost.ActualCost, 1e-12)
		})
	}
}

func TestPlatformPricingFallback_SameSKUEmbeddingInputOnlyBehaviorStaysAvailable(t *testing.T) {
	catalog := newStubPricingServiceFromJSON(t, `{
		"embedding-own-sku": {"mode":"embedding","input_cost_per_token":0.000001}
	}`)
	billing := platformFallbackTestBilling(catalog)
	billing.frozenPricingFallbackModels = map[string]string{PlatformOpenAI: ""}
	price, err := billing.GetModelPricingForPlatform(context.Background(), PlatformOpenAI, "embedding-own-sku")
	require.NoError(t, err)
	requirePlatformFallbackRates(t, price, 1e-6, 0)
	require.Empty(t, price.FallbackModel)
	group := &Group{ID: 77, Platform: PlatformOpenAI, RateMultiplier: 1}
	cost, err := billing.CalculateTokenCostForRequest(TokenCostRequest{
		Ctx: context.Background(), Group: group, Model: "embedding-own-sku", RateMultiplier: 1,
		Tokens: UsageTokens{InputTokens: 1000}, Resolver: NewModelPricingResolver(nil, billing),
	})
	require.NoError(t, err)
	require.InDelta(t, 0.001, cost.TotalCost, 1e-12)
}

func TestPlatformPricingFallback_FinalTargetsCannotExposeNonFinitePrices(t *testing.T) {
	for _, value := range []float64{math.NaN(), math.Inf(1), math.Inf(-1)} {
		for _, dimension := range []string{"input", "output", "cache", "long context"} {
			t.Run(dimension+"/"+fmtFloatForFallbackTest(value), func(t *testing.T) {
				entry := &LiteLLMModelPricing{InputCostPerToken: 1e-6, OutputCostPerToken: 2e-6}
				switch dimension {
				case "input":
					entry.InputCostPerToken = value
				case "output":
					entry.OutputCostPerToken = value
				case "cache":
					entry.CacheReadInputTokenCost = value
				case "long context":
					entry.LongContextInputCostMultiplier = value
				}
				billing := platformFallbackTestBilling(&PricingService{pricingData: map[string]*LiteLLMModelPricing{"fixture-fallback": entry}})
				billing.frozenPricingFallbackModels = map[string]string{PlatformOpenAI: "fixture-fallback"}
				price, err := billing.GetModelPricingForPlatform(context.Background(), PlatformOpenAI, "unpublished-request-sku")
				require.ErrorIs(t, err, ErrModelPricingUnavailable)
				require.Nil(t, price)
			})
		}
	}
}

func fmtFloatForFallbackTest(value float64) string {
	if math.IsNaN(value) {
		return "NaN"
	}
	if math.IsInf(value, 1) {
		return "+Inf"
	}
	return "-Inf"
}
