//go:build unit || !integration

package service

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

// A channel may price the ultrafast tier independently of Fast/priority. The
// override must reach both billing paths (legacy channel card and the unified
// resolver), must leave the other tiers untouched, and must not mutate the
// model-owned default.
func TestChannelUltrafastPricingOverridesBothBillingPaths(t *testing.T) {
	svc := &BillingService{fallbackPrices: make(map[string]*ModelPricing)}
	svc.initFallbackPricing()
	tokens := UsageTokens{InputTokens: 1_000_000, OutputTokens: 1_000_000, CacheCreationTokens: 1_000_000, CacheReadTokens: 1_000_000}
	for _, tc := range []struct {
		name string
		card string
		want float64
	}{
		{"unset keeps the model default", `{}`, 6},
		{"explicit null keeps the model default", `{"ultrafast_multiplier":null}`, 6},
		{"custom multiplier", `{"ultrafast_multiplier":3}`, 3},
		{"standard price", `{"ultrafast_multiplier":1}`, 1},
		{"fractional multiplier", `{"ultrafast_multiplier":1.25}`, 1.25},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var card ChannelModelPricing
			require.NoError(t, json.Unmarshal([]byte(tc.card), &card))
			card.Platform = PlatformOpenAI
			card.Models = []string{"gpt-6-astra"}
			card.InputPrice = pricingMultiplier(1e-6)
			card.OutputPrice = pricingMultiplier(2e-6)
			card.CacheWritePrice = pricingMultiplier(3e-6)
			card.CacheReadPrice = pricingMultiplier(0.1e-6)
			card.FastMultiplier = pricingMultiplier(2.5)
			card.FlexMultiplier = pricingMultiplier(0.4)
			groupID := int64(8907)
			resolver := NewModelPricingResolver(newChannelServiceWithPricings(groupID, []ChannelModelPricing{card}), svc)
			for name, calculate := range map[string]func(string) (*CostBreakdown, error){
				"legacy": func(tier string) (*CostBreakdown, error) {
					return svc.calculateCostInternalWithPolicy("gpt-6-astra", tokens, 1, tier, &card, false)
				},
				"unified": func(tier string) (*CostBreakdown, error) {
					return svc.CalculateCostUnified(CostInput{
						Ctx: context.Background(), Model: "gpt-6-astra", GroupID: &groupID,
						Tokens: tokens, RateMultiplier: 1, ServiceTier: tier, Resolver: resolver,
						DisableModelLongContextPricing: true,
					})
				},
			} {
				t.Run(name, func(t *testing.T) {
					cost, err := calculate(" UltraFast ")
					require.NoError(t, err)
					require.InDelta(t, tc.want, cost.InputCost, 1e-12)
					require.InDelta(t, 2*tc.want, cost.OutputCost, 1e-12)
					require.InDelta(t, 3*tc.want, cost.CacheCreationCost, 1e-12)
					require.InDelta(t, 0.1*tc.want, cost.CacheReadCost, 1e-12)
					// The ultrafast override must not leak into the other tiers.
					for tier, multiplier := range map[string]float64{"priority": 2.5, "flex": 0.4, "default": 1} {
						control, err := calculate(tier)
						require.NoError(t, err)
						require.InDelta(t, multiplier, control.InputCost, 1e-12)
					}
				})
			}
			card.Intervals = []PricingInterval{{MinTokens: 100, InputMultiplier: pricingMultiplier(2)}}
			resolver = NewModelPricingResolver(newChannelServiceWithPricings(groupID, []ChannelModelPricing{card}), svc)
			intervalCost, err := svc.CalculateCostUnified(CostInput{
				Ctx: context.Background(), Model: "gpt-6-astra", GroupID: &groupID,
				Tokens: tokens, RateMultiplier: 1, ServiceTier: "ultrafast", Resolver: resolver,
				DisableModelLongContextPricing: true,
			})
			require.NoError(t, err)
			require.InDelta(t, 2*tc.want, intervalCost.InputCost, 1e-12)
			// A channel override must not mutate the model's shared default.
			catalog, err := svc.GetModelPricing("gpt-6-astra")
			require.NoError(t, err)
			require.Equal(t, 6.0, configuredServiceTierMultiplier("ultrafast", catalog))
		})
	}
}

// The override is only meaningful for the ultrafast tier; a channel that only
// configures it must still be treated as configured for pricing resolution.
func TestChannelUltrafastPricingCountsAsConfiguredOverride(t *testing.T) {
	multiplier := 2.5
	require.True(t, hasConfiguredPricingOverrides(&ChannelModelPricing{UltrafastMultiplier: &multiplier}))
	require.False(t, hasConfiguredPricingOverrides(&ChannelModelPricing{}))
}
