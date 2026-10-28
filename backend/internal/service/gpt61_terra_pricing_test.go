//go:build unit

package service

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/config"
	"github.com/stretchr/testify/require"
)

// Expectations come from the reviewed 2026-10-28 API price/model pages, rather
// than another fallback implementation:
// https://developers.openai.com/api/docs/pricing
// https://developers.openai.com/api/docs/models/gpt-6.1-terra
func TestGPT61TerraReleaseCatalogRatesAndAPILimits(t *testing.T) {
	data, err := os.ReadFile("../../resources/model-pricing/model_prices_and_context_window.json")
	require.NoError(t, err)
	var catalog map[string]map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(data, &catalog))
	card := catalog["gpt-6.1-terra"]
	require.NotNil(t, card)
	for field, want := range map[string]float64{
		"input_cost_per_token":                     1e-6,
		"output_cost_per_token":                    5e-6,
		"cache_read_input_token_cost":              0.05e-6,
		"cache_creation_input_token_cost":          1.25e-6,
		"input_cost_per_token_priority":            2e-6,
		"output_cost_per_token_priority":           10e-6,
		"cache_read_input_token_cost_priority":     0.1e-6,
		"cache_creation_input_token_cost_priority": 2.5e-6,
		"input_cost_per_token_flex":                0.5e-6,
		"output_cost_per_token_flex":               2.5e-6,
		"cache_read_input_token_cost_flex":         0.025e-6,
		"cache_creation_input_token_cost_flex":     0.625e-6,
		"input_cost_per_token_batches":             0.5e-6,
		"output_cost_per_token_batches":            2.5e-6,
		"cache_read_input_token_cost_batches":      0.025e-6,
		"cache_creation_input_token_cost_batches":  0.625e-6,
		"long_context_input_token_threshold":       272000,
		"long_context_input_cost_multiplier":       2,
		"long_context_output_cost_multiplier":      1.5,
		"context_window":                           1050000,
		"max_input_tokens":                         922000,
		"max_output_tokens":                        128000,
	} {
		t.Run(field, func(t *testing.T) {
			var actual float64
			require.NoError(t, json.Unmarshal(card[field], &actual))
			require.InDelta(t, want, actual, 1e-15)
		})
	}
}

func TestGPT61TerraPricingAliasesAndUnknownSKUIsolation(t *testing.T) {
	pricing := &PricingService{pricingData: map[string]*LiteLLMModelPricing{
		"gpt-6.1-sol":   {InputCostPerToken: 2e-6, OutputCostPerToken: 10e-6},
		"gpt-5.6-terra": {InputCostPerToken: 2e-6, OutputCostPerToken: 12e-6},
		"gpt-6.1":       {InputCostPerToken: 999e-6, OutputCostPerToken: 999e-6},
		"gpt-5.1-codex": {InputCostPerToken: 999e-6, OutputCostPerToken: 999e-6},
		"claude-opus-5": {InputCostPerToken: 999e-6, OutputCostPerToken: 999e-6},
	}}
	billing := NewBillingService(&config.Config{}, pricing)
	fallbackBilling := NewBillingService(&config.Config{}, nil)
	for _, model := range []string{
		"gpt-6.1-terra", "openai/GPT-6.1-Terra-max", "gpt_6.1_terra",
		"gpt-6.1-terra-openai-compact", "openai/gpt-6.1-terra-low",
	} {
		t.Run(model, func(t *testing.T) {
			cost, err := billing.CalculateCost(model, UsageTokens{InputTokens: 1000, OutputTokens: 200}, 1)
			require.NoError(t, err)
			require.InDelta(t, 0.002, cost.TotalCost, 1e-12, "Terra must use its own $1 input / $5 output card")
			card, matched := billing.IdentifiedSameModelPricing(model)
			require.NotNil(t, card)
			require.Equal(t, "gpt-6.1-terra", matched)
		})
	}
	for _, model := range []string{
		"gpt-6.1-terrain", "gpt-6.1-terra-preview", "gpt-6.1-terra-pro",
		"gpt-6.1-terra-ultra", "gpt-6.1-terra-2026-10-28",
		"gpt-6.1-terra-codex", "gpt-6.1-terra-claude", "gpt-6.1-terra-claude-opus-5",
		"openai/GPT_6.1_TERRA_CODEX", "gpt-6.1-terra-gpt-5.4",
	} {
		t.Run(model, func(t *testing.T) {
			require.Nil(t, pricing.GetModelPricing(model), "unregistered SKU cannot inherit a family/default price")
			require.Nil(t, pricing.GetIdentifiedModelPricing(model))
			for _, source := range []*BillingService{billing, fallbackBilling} {
				cost, err := source.CalculateCost(model, UsageTokens{InputTokens: 1000}, 1)
				require.ErrorIs(t, err, ErrModelPricingUnavailable)
				require.Nil(t, cost)
				require.False(t, source.HasIdentifiedTokenPricing(model))
				card, matched := source.IdentifiedSameModelPricing(model)
				require.Nil(t, card, "a cross-family fallback must not be reported as a same-SKU reference")
				require.Empty(t, matched)
			}
		})
	}
	// An operator may explicitly price a distinct SKU. Only inferred fallback
	// is forbidden; exact configuration remains authoritative.
	for _, model := range []string{"gpt-6.1-terra-preview", "gpt-6.1-terra-codex", "gpt-6.1-terra-claude-opus-5"} {
		t.Run("explicit price/"+model, func(t *testing.T) {
			pricing.mu.Lock()
			pricing.pricingData[model] = &LiteLLMModelPricing{InputCostPerToken: 3e-6, OutputCostPerToken: 7e-6}
			pricing.mu.Unlock()
			cost, err := billing.CalculateCost(model, UsageTokens{InputTokens: 1000, OutputTokens: 200}, 1)
			require.NoError(t, err)
			require.InDelta(t, 0.0044, cost.TotalCost, 1e-12)
			require.True(t, billing.HasIdentifiedTokenPricing(model))
		})
	}
}

func TestGPT61TerraExplicitCatalogCachePricesOverrideFallback(t *testing.T) {
	for _, cachePrice := range []string{"0", "0.000004"} {
		pricing := &PricingService{}
		var err error
		pricing.pricingData, err = pricing.parsePricingData([]byte(`{"gpt-6.1-terra":{"litellm_provider":"openai","input_cost_per_token":0.000003,"output_cost_per_token":0.000007,"cache_read_input_token_cost":0.000002,"cache_creation_input_token_cost":` + cachePrice + `,"cache_creation_input_token_cost_priority":0.000099}}`))
		require.NoError(t, err)
		billing := NewBillingService(&config.Config{}, pricing)
		for _, model := range []string{"gpt-6.1-terra", "openai/gpt-6.1-terra-max"} {
			for tier, multiplier := range map[string]float64{"": 1, "fast": 2, "priority": 2, "flex": 0.5} {
				cost, err := billing.CalculateCostWithServiceTier(model, UsageTokens{InputTokens: 1000, OutputTokens: 1000, CacheReadTokens: 1000, CacheCreationTokens: 1000}, 1, tier)
				require.NoError(t, err)
				require.InDelta(t, 0.003*multiplier, cost.InputCost, 1e-12)
				require.InDelta(t, 0.007*multiplier, cost.OutputCost, 1e-12)
				require.InDelta(t, 0.002*multiplier, cost.CacheReadCost, 1e-12)
				if cachePrice == "0" {
					require.Zero(t, cost.CacheCreationCost, "explicit free cache writes must remain free in every tier")
				} else {
					require.InDelta(t, 0.004*multiplier, cost.CacheCreationCost, 1e-12)
				}
			}
		}
	}
}

func TestGPT61TerraExplicitZeroCacheReadRemainsFreeAcrossTiers(t *testing.T) {
	pricing := &PricingService{}
	var err error
	pricing.pricingData, err = pricing.parsePricingData([]byte(`{"gpt-6.1-terra":{"litellm_provider":"openai","input_cost_per_token":0.000003,"output_cost_per_token":0.000007,"cache_read_input_token_cost":0,"cache_read_input_token_cost_priority":0.000099,"cache_creation_input_token_cost":0.000004}}`))
	require.NoError(t, err)
	billing := NewBillingService(&config.Config{}, pricing)
	for _, model := range []string{"gpt-6.1-terra", "openai/gpt-6.1-terra-max"} {
		for tier, multiplier := range map[string]float64{"": 1, "fast": 2, "priority": 2, "flex": 0.5} {
			cost, err := billing.CalculateCostWithServiceTier(model, UsageTokens{InputTokens: 1000, OutputTokens: 1000, CacheReadTokens: 1000, CacheCreationTokens: 1000}, 1, tier)
			require.NoError(t, err)
			require.Zero(t, cost.CacheReadCost, "explicit free cache reads must remain free even with a conflicting priority catalog price")
			require.InDelta(t, 0.014*multiplier, cost.TotalCost, 1e-12)
		}
	}
}

func TestGPT61TerraAcceptedPricingSurvivesCatalogReloadAndDurableSettlement(t *testing.T) {
	const model = "openai/gpt-6.1-terra-max"
	pricing := &PricingService{pricingData: map[string]*LiteLLMModelPricing{}}
	billing := NewBillingService(&config.Config{}, pricing)
	group := &Group{ID: 7, Platform: PlatformOpenAI, RateMultiplier: 1}
	accepted := withBillingPricingSnapshot(context.Background(), &APIKey{Group: group}, time.Date(2026, 10, 28, 0, 0, 0, 0, time.UTC), billing, nil, nil, model)
	// Publish another immutable generation after admission. Settlement must keep
	// the accepted Terra card, including cached input, Fast and the strict tier.
	pricing.mu.Lock()
	pricing.pricingData = map[string]*LiteLLMModelPricing{
		"gpt-6.1-terra": {InputCostPerToken: 100e-6, OutputCostPerToken: 500e-6},
	}
	pricing.mu.Unlock()
	stored, err := ExportBillingPricingSnapshot(accepted, model)
	require.NoError(t, err)
	restored, err := RestoreBillingPricingSnapshot(context.Background(), stored)
	require.NoError(t, err)
	for _, ctx := range []context.Context{accepted, restored} {
		for tier, want := range map[string]float64{"": 0.544452, "priority": 1.088904, "flex": 0.272226} {
			cost, err := billing.CalculateTokenCostForRequest(TokenCostRequest{
				Ctx: ctx, Model: model, RateMultiplier: 1, ServiceTier: tier,
				Tokens: UsageTokens{InputTokens: 269001, CacheReadTokens: 2000, CacheCreationTokens: 1000, OutputTokens: 500},
			})
			require.NoError(t, err)
			require.InDelta(t, want, cost.TotalCost, 1e-12)
			require.True(t, cost.LongContextBillingApplied)
		}
	}
}
