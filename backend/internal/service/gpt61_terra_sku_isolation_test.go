//go:build unit

package service

import (
	"context"
	"testing"

	"github.com/LuckyKuang/sub2api-plus/internal/config"
	"github.com/stretchr/testify/require"
)

func TestGPT61TerraUnknownExactCatalogKeepsItsTierPrices(t *testing.T) {
	pricing := &PricingService{}
	var err error
	pricing.pricingData, err = pricing.parsePricingData([]byte(`{"gpt-6.1-terra-gpt-5.4":{"litellm_provider":"openai","input_cost_per_token":0.000003,"output_cost_per_token":0.000007,"input_cost_per_token_priority":0.000011,"output_cost_per_token_priority":0.000013}}`))
	require.NoError(t, err)
	billing := NewBillingService(&config.Config{}, pricing)
	cost, err := billing.CalculateCostWithServiceTier("gpt-6.1-terra-gpt-5.4", UsageTokens{InputTokens: 1000, OutputTokens: 200}, 1, "priority")
	require.NoError(t, err)
	require.InDelta(t, 0.0136, cost.TotalCost, 1e-12, "an exact unregistered SKU price cannot inherit GPT-5.4's enforced Fast ratio")
}

func TestGPT61TerraUnknownSKUDoesNotBorrowChannelModelPrice(t *testing.T) {
	for _, model := range []string{"gpt-6.1-terra-codex", "gpt-6.1-terra-gpt-5.4", "openai/GPT_6.1_TERRA_CODEX"} {
		t.Run(model, func(t *testing.T) {
			billing, resolver := newTokenCostTestEnv(t, PlatformOpenAI, []ChannelModelPricing{{
				Platform: PlatformOpenAI, Models: []string{"gpt-5.3-codex", "gpt-5.4"}, BillingMode: BillingModeToken,
				InputPrice: testPtrFloat64(3e-6), OutputPrice: testPtrFloat64(7e-6),
			}}, nil)
			group := &Group{ID: 100, Platform: PlatformOpenAI}
			resolved := resolver.Resolve(context.Background(), PricingInput{Model: model, GroupID: &group.ID, Group: group})
			require.NotEqual(t, PricingSourceChannel, resolved.Source, "an unknown Terra SKU cannot select another model's channel card")
			require.Nil(t, resolved.BasePricing)
			cost, err := billing.CalculateTokenCostForRequest(TokenCostRequest{
				Ctx: context.Background(), Model: model, Group: group, Resolver: resolver, Resolved: resolved,
				Tokens: UsageTokens{InputTokens: 1000, OutputTokens: 200}, RateMultiplier: 1,
			})
			require.ErrorIs(t, err, ErrModelPricingUnavailable)
			require.Nil(t, cost)
		})
	}
}

func TestGPT61TerraUnknownSKURetainsItsExplicitChannelPrice(t *testing.T) {
	const model = "gpt-6.1-terra-codex"
	billing, resolver := newTokenCostTestEnv(t, PlatformOpenAI, []ChannelModelPricing{
		{
			Platform: PlatformOpenAI, Models: []string{"gpt-5.3-codex"}, BillingMode: BillingModeToken,
			InputPrice: testPtrFloat64(999e-6), OutputPrice: testPtrFloat64(999e-6),
		},
		{
			Platform: PlatformOpenAI, Models: []string{model}, BillingMode: BillingModeToken,
			InputPrice: testPtrFloat64(3e-6), OutputPrice: testPtrFloat64(7e-6),
		},
	}, nil)
	group := &Group{ID: 100, Platform: PlatformOpenAI}
	resolved := resolver.Resolve(context.Background(), PricingInput{Model: model, GroupID: &group.ID, Group: group})
	require.Equal(t, PricingSourceChannel, resolved.Source)
	cost, err := billing.CalculateTokenCostForRequest(TokenCostRequest{
		Ctx: context.Background(), Model: model, Group: group, Resolver: resolver, Resolved: resolved,
		Tokens: UsageTokens{InputTokens: 1000, OutputTokens: 200}, RateMultiplier: 1,
	})
	require.NoError(t, err)
	require.InDelta(t, 0.0044, cost.TotalCost, 1e-12)

	// Known effort/provider aliases continue to inherit their own base card.
	billing, resolver = newTokenCostTestEnv(t, PlatformOpenAI, []ChannelModelPricing{{
		Platform: PlatformOpenAI, Models: []string{"gpt-6.1-terra"}, BillingMode: BillingModeToken,
		InputPrice: testPtrFloat64(3e-6), OutputPrice: testPtrFloat64(7e-6),
	}}, nil)
	const alias = "openai/gpt-6.1-terra-max"
	resolved = resolver.Resolve(context.Background(), PricingInput{Model: alias, GroupID: &group.ID, Group: group})
	require.Equal(t, PricingSourceChannel, resolved.Source)
	cost, err = billing.CalculateTokenCostForRequest(TokenCostRequest{
		Ctx: context.Background(), Model: alias, Group: group, Resolver: resolver, Resolved: resolved,
		Tokens: UsageTokens{InputTokens: 1000, OutputTokens: 200}, RateMultiplier: 1,
	})
	require.NoError(t, err)
	require.InDelta(t, 0.0044, cost.TotalCost, 1e-12)
}

func TestGPT61TerraUsageBillingCandidatesDoNotInventAnotherSKU(t *testing.T) {
	for _, tc := range []struct {
		model string
		want  []string
	}{
		{"gpt-6.1-terra-codex", []string{"gpt-6.1-terra-codex"}},
		{"gpt-6.1-terra-gpt-5.4", []string{"gpt-6.1-terra-gpt-5.4"}},
		{"gpt-6.1-terra-claude-opus-5", []string{"gpt-6.1-terra-claude-opus-5"}},
		{"openai/GPT_6.1_TERRA_CODEX", []string{"openai/GPT_6.1_TERRA_CODEX", "gpt-6.1-terra-codex"}},
		{"openai/gpt-6.1-terra-max", []string{"openai/gpt-6.1-terra-max", "gpt-6.1-terra-max", "gpt-6.1-terra"}},
	} {
		t.Run(tc.model, func(t *testing.T) {
			require.Equal(t, tc.want, usageBillingModelCandidates(tc.model))
		})
	}
	// A separately declared mapping/response candidate retains its configured
	// billing role; the guard removes only an inferred same-SKU normalization.
	require.Equal(t, []string{"gpt-6.1-terra-codex", "gpt-5.3-codex"},
		usageBillingModelCandidates("gpt-6.1-terra-codex", "gpt-5.3-codex"))
}
