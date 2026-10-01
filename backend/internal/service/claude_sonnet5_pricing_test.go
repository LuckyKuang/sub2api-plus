//go:build unit

package service

import (
	"context"
	"testing"

	"github.com/LuckyKuang/sub2api-plus/internal/config"
	"github.com/stretchr/testify/require"
)

func TestClaudeSonnet5ReferenceFallbackAndCatalogAgree(t *testing.T) {
	for _, svc := range []*ChannelPricingReferenceService{newReferenceService(t, `{"gpt-5.4":{"litellm_provider":"openai","input_cost_per_token":0.0000025,"output_cost_per_token":0.000015}}`), newBundledCatalogReferenceService(t)} {
		refs, err := svc.List(context.Background(), PlatformAnthropic)
		require.NoError(t, err)
		listed := referencesByModel(t, refs)["claude-sonnet-5"]
		ref, err := svc.Resolve(context.Background(), PlatformAnthropic, "claude-sonnet-5")
		require.NoError(t, err)
		require.Equal(t, listed, ref)
		require.Equal(t, ChannelPricingStatusPriced, ref.Status)
		require.Equal(t, "claude-sonnet-5", ref.MatchedModel)
		require.InDelta(t, 2e-6, *ref.Pricing.InputPrice, 1e-15)
		require.InDelta(t, 10e-6, *ref.Pricing.OutputPrice, 1e-15)
		require.InDelta(t, 2.5e-6, *ref.Pricing.CacheWritePrice, 1e-15)
		require.InDelta(t, 4e-6, *ref.Pricing.CacheWrite1hPrice, 1e-15)
		require.InDelta(t, 0.2e-6, *ref.Pricing.CacheReadPrice, 1e-15)
		for _, alias := range []string{"claude-sonnet-5-thinking", "anthropic/claude-sonnet-5", "anthropic/claude-sonnet-5-thinking"} {
			aliasRef, err := svc.Resolve(context.Background(), PlatformAnthropic, alias)
			require.NoError(t, err)
			require.Equal(t, ref.Status, aliasRef.Status, alias)
			require.Equal(t, ref.Source, aliasRef.Source, alias)
			require.Equal(t, ref.Pricing.Platform, aliasRef.Pricing.Platform, alias)
			require.Equal(t, ref.Pricing.BillingMode, aliasRef.Pricing.BillingMode, alias)
			require.Equal(t, ref.Pricing.InputPrice, aliasRef.Pricing.InputPrice, alias)
			require.Equal(t, ref.Pricing.OutputPrice, aliasRef.Pricing.OutputPrice, alias)
			require.Equal(t, ref.Pricing.CacheWritePrice, aliasRef.Pricing.CacheWritePrice, alias)
			require.Equal(t, ref.Pricing.CacheWrite1hPrice, aliasRef.Pricing.CacheWrite1hPrice, alias)
			require.Equal(t, ref.Pricing.CacheReadPrice, aliasRef.Pricing.CacheReadPrice, alias)
			require.Equal(t, []string{alias}, aliasRef.Pricing.Models)
			require.Equal(t, ref.MatchedModel, aliasRef.MatchedModel, alias)
		}
		for _, model := range []string{"claude-connect-5", "claude-sonnet-5-preview", "claude-sonnet-5-unknown", "claude-sonnet-50"} {
			unknown, err := svc.Resolve(context.Background(), PlatformAnthropic, model)
			require.NoError(t, err)
			require.Equal(t, ChannelPricingStatusManualRequired, unknown.Status, model)
			require.Nil(t, unknown.Pricing, model)
		}
	}
}

func TestClaudeSonnet5BillingCacheBreakdownAndSKUIsolation(t *testing.T) {
	catalog := &PricingService{pricingData: map[string]*LiteLLMModelPricing{}}
	for _, billing := range []*BillingService{NewBillingService(&config.Config{}, nil), NewBillingService(&config.Config{}, catalog)} {
		for _, model := range []string{"claude-sonnet-5", "claude-sonnet-5-thinking", "anthropic/claude-sonnet-5"} {
			pricing, err := billing.GetModelPricing(model)
			require.NoError(t, err)
			require.True(t, pricing.SupportsCacheBreakdown)
			cost, err := billing.CalculateCost(model, UsageTokens{InputTokens: 1000, OutputTokens: 1000, CacheReadTokens: 1000, CacheCreationTokens: 2000, CacheCreation5mTokens: 1000, CacheCreation1hTokens: 1000}, 1)
			require.NoError(t, err)
			require.InDelta(t, 0.0187, cost.TotalCost, 1e-12)
		}
		require.NotSame(t, billing.getFallbackPricing("claude-sonnet-5"), billing.getFallbackPricing("claude-sonnet-5-5"))
	}
	sonnet5 := catalog.GetModelPricing("claude-sonnet-5")
	require.Same(t, claudeSonnet5FallbackPricing, sonnet5)
	require.NotSame(t, sonnet5, catalog.GetModelPricing("claude-sonnet-5-5"))
	for _, model := range []string{"claude-connect-5", "claude-sonnet-5-preview", "claude-sonnet-5-unknown"} {
		require.False(t, isClaudeSonnet5Model(model))
		require.NotSame(t, sonnet5, catalog.GetModelPricing(model))
	}
}
