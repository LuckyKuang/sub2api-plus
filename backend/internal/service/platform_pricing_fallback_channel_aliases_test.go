//go:build unit

package service

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestPlatformPricingFallback_ChannelUnknownSuffixCannotBorrowBaseRetailPrice(t *testing.T) {
	for _, tc := range []struct{ model, base string }{
		{"gpt-5.4-preview", "gpt-5.4"},
		{"openai/gpt-5.4-preview", "gpt-5.4"},
		{"gpt-5.6-luna-preview", "gpt-5.6-luna"},
		{"unpublished-codex-model", "gpt-5.3-codex"},
	} {
		for _, fallback := range []string{"grok-4.6", ""} {
			t.Run(tc.model+"/"+fallback, func(t *testing.T) {
				billing, resolver := newTokenCostTestEnv(t, PlatformOpenAI, []ChannelModelPricing{{
					Platform: PlatformOpenAI, Models: []string{tc.base}, BillingMode: BillingModeToken,
					InputPrice: billingFloat(99e-6), OutputPrice: billingFloat(199e-6),
				}}, nil)
				billing.frozenPricingFallbackModels = map[string]string{PlatformOpenAI: fallback}
				group := &Group{ID: 100, Platform: PlatformOpenAI, RateMultiplier: 1}
				resolved := resolver.Resolve(context.Background(), PricingInput{Model: tc.model, Group: group, GroupID: &group.ID})
				require.NotEqual(t, PricingSourceChannel, resolved.Source)
				cost, err := billing.CalculateTokenCostForRequest(TokenCostRequest{
					Ctx: context.Background(), Model: tc.model, Group: group, Resolver: resolver, Resolved: resolved,
					Tokens: UsageTokens{InputTokens: 1000, OutputTokens: 1000}, RateMultiplier: 1,
				})
				if fallback == "" {
					require.ErrorIs(t, err, ErrModelPricingUnavailable)
					require.Nil(t, cost)
				} else {
					require.NoError(t, err)
					require.Equal(t, PricingSourcePlatformFallback, resolved.Source)
					require.Equal(t, "grok-4.6", resolved.BasePricing.FallbackModel)
					require.InDelta(t, .008, cost.TotalCost, 1e-12)
				}
			})
		}
	}
}

func TestPlatformPricingFallback_ChannelLiteralAndWildcardCardsPrecedeAliasAndFallback(t *testing.T) {
	for _, tc := range []struct {
		name, pattern        string
		input, output, total float64
	}{
		{"literal preview", "gpt-5.4-preview", 7e-6, 11e-6, .018},
		{"literal free preview", "gpt-5.4-preview", 0, 0, 0},
		{"explicit preview wildcard", "gpt-5.4-*", 4e-6, 6e-6, .010},
	} {
		t.Run(tc.name, func(t *testing.T) {
			billing, resolver := newTokenCostTestEnv(t, PlatformOpenAI, []ChannelModelPricing{
				{Platform: PlatformOpenAI, Models: []string{"gpt-5.4"}, BillingMode: BillingModeToken,
					InputPrice: billingFloat(99e-6), OutputPrice: billingFloat(199e-6)},
				{Platform: PlatformOpenAI, Models: []string{tc.pattern}, BillingMode: BillingModeToken,
					InputPrice: billingFloat(tc.input), OutputPrice: billingFloat(tc.output)},
			}, nil)
			billing.frozenPricingFallbackModels = map[string]string{PlatformOpenAI: "grok-4.6"}
			group := &Group{ID: 100, Platform: PlatformOpenAI, RateMultiplier: 1}
			resolved := resolver.Resolve(context.Background(), PricingInput{Model: "gpt-5.4-preview", Group: group, GroupID: &group.ID})
			require.Equal(t, PricingSourceChannel, resolved.Source)
			cost, err := billing.CalculateTokenCostForRequest(TokenCostRequest{
				Ctx: context.Background(), Model: "gpt-5.4-preview", Group: group, Resolver: resolver, Resolved: resolved,
				Tokens: UsageTokens{InputTokens: 1000, OutputTokens: 1000}, RateMultiplier: 1,
			})
			require.NoError(t, err)
			require.InDelta(t, tc.total, cost.TotalCost, 1e-12)
		})
	}
}

func TestPlatformPricingFallback_ChannelRegisteredEffortDateAndProviderAliasesRetainRetailPrice(t *testing.T) {
	for _, tc := range []struct{ model, base string }{
		{"gpt-5.4-high", "gpt-5.4"},
		{"openai/gpt-5.4-low", "gpt-5.4"},
		{"gpt-5.4-2026-03-05", "gpt-5.4"},
		{"gpt-5.4-20260305", "gpt-5.4"},
		{"openai/gpt-5.6-luna-high", "gpt-5.6-luna"},
	} {
		t.Run(tc.model, func(t *testing.T) {
			billing, resolver := newTokenCostTestEnv(t, PlatformOpenAI, []ChannelModelPricing{{
				Platform: PlatformOpenAI, Models: []string{tc.base}, BillingMode: BillingModeToken,
				InputPrice: billingFloat(7e-6), OutputPrice: billingFloat(11e-6),
			}}, nil)
			billing.frozenPricingFallbackModels = map[string]string{PlatformOpenAI: "grok-4.6"}
			group := &Group{ID: 100, Platform: PlatformOpenAI, RateMultiplier: 1}
			resolved := resolver.Resolve(context.Background(), PricingInput{Model: tc.model, Group: group, GroupID: &group.ID})
			require.Equal(t, PricingSourceChannel, resolved.Source)
			cost, err := billing.CalculateTokenCostForRequest(TokenCostRequest{
				Ctx: context.Background(), Model: tc.model, Group: group, Resolver: resolver, Resolved: resolved,
				Tokens: UsageTokens{InputTokens: 1000, OutputTokens: 1000}, RateMultiplier: 1,
			})
			require.NoError(t, err)
			require.InDelta(t, .018, cost.TotalCost, 1e-12)
		})
	}
}

func TestPlatformPricingFallback_UsageCandidatesKeepLiteralOrderAndOnlyRegisteredAliases(t *testing.T) {
	require.Equal(t, []string{"gpt-5.4-preview"}, usageBillingModelCandidates("gpt-5.4-preview"))
	require.Equal(t, []string{"gpt-5.4-preview", "gpt-5.4-mini"},
		usageBillingModelCandidates("gpt-5.4-preview", "gpt-5.4-mini"))
	require.Equal(t, []string{"gpt-5.4-2026-03-05", "gpt-5.4", "grok-4.6"},
		usageBillingModelCandidates("gpt-5.4-2026-03-05", "grok-4.6"))
}

func TestPlatformPricingFallback_OpenAIUsageCandidatesCannotReviveDisabledFallback(t *testing.T) {
	for _, alternate := range []string{"", "gpt-5.4-mini"} {
		t.Run(alternate, func(t *testing.T) {
			billing := platformFallbackTestBilling(nil)
			billing.frozenPricingFallbackModels = map[string]string{PlatformOpenAI: ""}
			svc := &OpenAIGatewayService{billingService: billing}
			group := &Group{ID: 77, Platform: PlatformOpenAI, RateMultiplier: 1}
			key := &APIKey{ID: 11, Group: group}
			cost, err := svc.calculateOpenAIRecordUsageCost(context.Background(),
				&OpenAIForwardResult{Model: "gpt-5.4-preview"}, key,
				usageBillingModelCandidates("gpt-5.4-preview", alternate), 1, 1, 1, 1,
				UsageTokens{InputTokens: 1000, OutputTokens: 1000}, "", nil, false,
				time.Date(2026, 10, 26, 0, 0, 0, 0, time.UTC))
			if alternate == "" {
				require.ErrorIs(t, err, ErrModelPricingUnavailable)
				require.Nil(t, cost)
			} else {
				require.NoError(t, err)
				require.InDelta(t, .00525, cost.TotalCost, 1e-12,
					"the explicitly supplied mini SKU retains its own $0.75/M and $4.50/M prices")
			}
		})
	}
}
