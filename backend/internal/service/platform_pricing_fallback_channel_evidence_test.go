//go:build unit

package service

import (
	"context"
	"testing"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/config"
	"github.com/stretchr/testify/require"
)

func platformFallbackChannelEvidenceServices(model, fallback string, card ChannelModelPricing) (*BillingService, *ChannelService, *Group) {
	group := &Group{ID: 79, Platform: PlatformOpenAI, RateMultiplier: 1, LongContextPricingEnabled: true}
	billing := NewBillingService(&config.Config{}, nil)
	billing.frozenPricingFallbackModels = map[string]string{PlatformOpenAI: fallback}
	card.Platform, card.Models = PlatformOpenAI, []string{model}
	cache := populateChannelCache([]Channel{{
		ID: 4, Status: StatusActive, GroupIDs: []int64{group.ID},
		RestrictModels: true, BillingModelSource: BillingModelSourceRequested,
		ModelPricing: []ChannelModelPricing{card},
	}}, map[int64]string{group.ID: PlatformOpenAI})
	return billing, &ChannelService{frozenCache: cache, billingService: billing}, group
}

func TestPlatformPricingFallbackChannelEvidence_UnpricedEditorCardsAreRejected(t *testing.T) {
	for _, tc := range []struct {
		name string
		card ChannelModelPricing
	}{
		{name: "empty token", card: ChannelModelPricing{BillingMode: BillingModeToken}},
		{name: "service tier multiplier only", card: ChannelModelPricing{FastMultiplier: billingFloat(2)}},
		{name: "context multiplier only", card: ChannelModelPricing{BillingMode: BillingModeToken, Intervals: []PricingInterval{{
			MinTokens: 0, InputMultiplier: billingFloat(2), OutputMultiplier: billingFloat(3),
		}}}},
		{name: "time multiplier only", card: ChannelModelPricing{BillingMode: BillingModeToken, TimePricing: &ChannelTimePricing{
			Timezone: "UTC", Periods: []ChannelTimePricingPeriod{{StartTime: "01:00", EndTime: "04:00", Multiplier: 2}},
		}}},
		{name: "empty per request", card: ChannelModelPricing{BillingMode: BillingModePerRequest}},
		{name: "token units cannot price requests", card: ChannelModelPricing{BillingMode: BillingModePerRequest, Intervals: []PricingInterval{{
			MinTokens: 0, InputPrice: billingFloat(2e-6), OutputPrice: billingFloat(3e-6),
		}}}},
		{name: "request units cannot price tokens", card: ChannelModelPricing{BillingMode: BillingModeToken, Intervals: []PricingInterval{{
			MinTokens: 0, PerRequestPrice: billingFloat(0.25),
		}}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			const model = "unpublished-channel-evidence-sku"
			billing, channels, group := platformFallbackChannelEvidenceServices(model, "", tc.card)
			ctx := context.Background()
			require.True(t, channels.IsModelRestricted(ctx, group.ID, model),
				"a matching editor card without a unit price must not authorize an unpriced upstream request")
			ctx = withBillingPricingSnapshot(ctx, &APIKey{Group: group, GroupID: &group.ID},
				time.Date(2026, 10, 28, 2, 0, 0, 0, time.UTC), billing, channels, nil, model)
			require.NoError(t, ValidateBillingPricingSnapshot(ctx))
			require.True(t, channels.IsModelRestricted(ctx, group.ID, model))
			cost, err := billing.CalculateCostUnified(CostInput{
				Ctx: ctx, Model: model, Group: group, GroupID: &group.ID,
				Tokens: UsageTokens{InputTokens: 1000, OutputTokens: 1000}, RequestCount: 2,
				RateMultiplier: 1, Resolver: NewModelPricingResolver(channels, billing),
			})
			require.ErrorIs(t, err, ErrModelPricingUnavailable)
			require.Nil(t, cost, "missing prices must not become an implicit free card")
		})
	}
}

func TestPlatformPricingFallbackChannelEvidence_EditorCardsInheritSystemOrPlatformPrice(t *testing.T) {
	for _, source := range []struct {
		name, model, fallback  string
		flatCost, intervalCost float64
	}{
		// Reviewed same-SKU card: $0.75/M input and $4.50/M output.
		{name: "same SKU system", model: "gpt-5.4-mini", flatCost: 0.00525, intervalCost: 0.015},
		// Explicit platform fallback: $2.50/M input and $15/M output.
		{name: "platform fallback", model: "unpublished-channel-evidence-sku", fallback: "gpt-5.4", flatCost: 0.0175, intervalCost: 0.05},
	} {
		for _, tc := range []struct {
			name string
			card ChannelModelPricing
			cost float64
		}{
			{name: "empty editor", card: ChannelModelPricing{BillingMode: BillingModeToken}, cost: source.flatCost},
			{name: "unused fast multiplier", card: ChannelModelPricing{FastMultiplier: billingFloat(2)}, cost: source.flatCost},
			{name: "context multipliers", card: ChannelModelPricing{BillingMode: BillingModeToken, Intervals: []PricingInterval{{
				MinTokens: 0, InputMultiplier: billingFloat(2), OutputMultiplier: billingFloat(3),
			}}}, cost: source.intervalCost},
		} {
			t.Run(source.name+"/"+tc.name, func(t *testing.T) {
				billing, channels, group := platformFallbackChannelEvidenceServices(source.model, source.fallback, tc.card)
				require.False(t, channels.IsModelRestricted(context.Background(), group.ID, source.model),
					"empty fields and multipliers inherit usable lower-priority unit prices")
				ctx := withBillingPricingSnapshot(context.Background(), &APIKey{Group: group, GroupID: &group.ID},
					time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC), billing, channels, nil, source.model)
				require.NoError(t, ValidateBillingPricingSnapshot(ctx))
				require.False(t, channels.IsModelRestricted(ctx, group.ID, source.model))
				cost, err := billing.CalculateCostUnified(CostInput{
					Ctx: ctx, Model: source.model, Group: group, GroupID: &group.ID,
					Tokens:         UsageTokens{InputTokens: 1000, OutputTokens: 1000},
					RateMultiplier: 1, Resolver: NewModelPricingResolver(channels, billing),
				})
				require.NoError(t, err)
				require.NotNil(t, cost)
				require.InDelta(t, tc.cost, cost.TotalCost, 1e-12)
				require.InDelta(t, tc.cost, cost.ActualCost, 1e-12)
			})
		}
	}
}

func TestPlatformPricingFallbackChannelEvidence_ExplicitZeroAndPricedIntervalsAreAdmitted(t *testing.T) {
	for _, tc := range []struct {
		name string
		card ChannelModelPricing
		cost float64
	}{
		{name: "explicit free token", card: ChannelModelPricing{BillingMode: BillingModeToken,
			InputPrice: billingFloat(0), OutputPrice: billingFloat(0)}},
		{name: "priced token interval", card: ChannelModelPricing{BillingMode: BillingModeToken, Intervals: []PricingInterval{{
			MinTokens: 0, InputPrice: billingFloat(2e-6), OutputPrice: billingFloat(3e-6),
		}}}, cost: 0.005},
		{name: "explicit free token interval", card: ChannelModelPricing{BillingMode: BillingModeToken, Intervals: []PricingInterval{{
			MinTokens: 0, InputPrice: billingFloat(0), OutputPrice: billingFloat(0),
		}}}},
		{name: "explicit free request", card: ChannelModelPricing{BillingMode: BillingModePerRequest, PerRequestPrice: billingFloat(0)}},
		{name: "priced request interval", card: ChannelModelPricing{BillingMode: BillingModePerRequest, Intervals: []PricingInterval{{
			MinTokens: 0, PerRequestPrice: billingFloat(0.25),
		}}}, cost: 0.5},
		{name: "explicit free request interval", card: ChannelModelPricing{BillingMode: BillingModePerRequest, Intervals: []PricingInterval{{
			MinTokens: 0, PerRequestPrice: billingFloat(0),
		}}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			const model = "unpublished-channel-evidence-sku"
			billing, channels, group := platformFallbackChannelEvidenceServices(model, "", tc.card)
			require.False(t, channels.IsModelRestricted(context.Background(), group.ID, model),
				"an explicit zero or an actual interval unit price establishes configured price evidence")
			ctx := withBillingPricingSnapshot(context.Background(), &APIKey{Group: group, GroupID: &group.ID},
				time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC), billing, channels, nil, model)
			require.NoError(t, ValidateBillingPricingSnapshot(ctx))
			require.False(t, channels.IsModelRestricted(ctx, group.ID, model))
			cost, err := billing.CalculateCostUnified(CostInput{
				Ctx: ctx, Model: model, Group: group, GroupID: &group.ID,
				Tokens: UsageTokens{InputTokens: 1000, OutputTokens: 1000}, RequestCount: 2,
				RateMultiplier: 1, Resolver: NewModelPricingResolver(channels, billing),
			})
			require.NoError(t, err)
			require.NotNil(t, cost)
			require.InDelta(t, tc.cost, cost.TotalCost, 1e-12)
			require.InDelta(t, tc.cost, cost.ActualCost, 1e-12)
		})
	}
}

func TestPlatformPricingFallbackChannelEvidence_NonTokenModesCannotBorrowTokenUnitPrices(t *testing.T) {
	for _, model := range []string{"unpublished-channel-evidence-sku", "gpt-5.4"} {
		for _, mode := range []BillingMode{BillingModePerRequest, BillingModeImage, BillingModeVideo} {
			for _, multiplierOnly := range []bool{false, true} {
				name := model + "/" + string(mode) + "/empty"
				card := ChannelModelPricing{BillingMode: mode}
				if multiplierOnly {
					name = model + "/" + string(mode) + "/multiplier only"
					card.FastMultiplier = billingFloat(2)
				}
				t.Run(name, func(t *testing.T) {
					billing, channels, group := platformFallbackChannelEvidenceServices(model, "gpt-5.4", card)
					require.True(t, channels.IsModelRestricted(context.Background(), group.ID, model),
						"a token price does not establish the declared request, image or video unit price")
					ctx := withBillingPricingSnapshot(context.Background(), &APIKey{Group: group, GroupID: &group.ID},
						time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC), billing, channels, nil, model)
					require.NoError(t, ValidateBillingPricingSnapshot(ctx))
					require.True(t, channels.IsModelRestricted(ctx, group.ID, model))
					cost, err := billing.CalculateCostUnified(CostInput{
						Ctx: ctx, Model: model, Group: group, GroupID: &group.ID,
						Tokens: UsageTokens{InputTokens: 1000, OutputTokens: 1000}, RequestCount: 2,
						RateMultiplier: 1, Resolver: NewModelPricingResolver(channels, billing),
					})
					require.ErrorIs(t, err, ErrModelPricingUnavailable)
					require.Nil(t, cost)
				})
			}
		}
	}
}

func TestPlatformPricingFallbackChannelEvidence_GroupUnitPricePrecedesAnUnpricedNonTokenChannel(t *testing.T) {
	for _, tc := range []struct {
		name string
		card ChannelModelPricing
		cost float64
	}{
		{name: "group token", card: ChannelModelPricing{BillingMode: BillingModeToken,
			InputPrice: billingFloat(2e-6), OutputPrice: billingFloat(3e-6)}, cost: 0.005},
		{name: "group request", card: ChannelModelPricing{BillingMode: BillingModePerRequest,
			PerRequestPrice: billingFloat(0.25)}, cost: 0.5},
	} {
		t.Run(tc.name, func(t *testing.T) {
			const model = "unpublished-channel-evidence-sku"
			billing, channels, group := platformFallbackChannelEvidenceServices(model, "gpt-5.4", ChannelModelPricing{
				BillingMode: BillingModePerRequest,
			})
			tc.card.Platform, tc.card.Models = PlatformOpenAI, []string{model}
			group.ModelPricing = []ChannelModelPricing{tc.card}
			ctx := withBillingPricingSnapshot(context.Background(), &APIKey{Group: group, GroupID: &group.ID},
				time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC), billing, channels, nil, model)
			require.NoError(t, ValidateBillingPricingSnapshot(ctx))
			require.False(t, channels.IsModelRestricted(ctx, group.ID, model),
				"the group's explicit unit price has priority over the lower channel's incomplete card")
			cost, err := billing.CalculateCostUnified(CostInput{
				Ctx: ctx, Model: model, Group: group, GroupID: &group.ID,
				Tokens: UsageTokens{InputTokens: 1000, OutputTokens: 1000}, RequestCount: 2,
				RateMultiplier: 1, Resolver: NewModelPricingResolver(channels, billing),
			})
			require.NoError(t, err)
			require.NotNil(t, cost)
			require.InDelta(t, tc.cost, cost.TotalCost, 1e-12)
			require.InDelta(t, tc.cost, cost.ActualCost, 1e-12)
		})
	}
}
