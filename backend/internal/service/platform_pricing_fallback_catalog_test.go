//go:build unit

package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestPlatformPricingFallback_CatalogDeepSeekQuotesRemainStandardWithTheActualTimeFactor(t *testing.T) {
	for _, platform := range []string{PlatformCommandCode, PlatformCline} {
		for _, tc := range []struct {
			name                 string
			at                   time.Time
			factor, expectedCost float64
		}{
			{"weekday peak", time.Date(2026, 10, 28, 2, 0, 0, 0, time.UTC), 2, 1.5},
			{"weekday off peak", time.Date(2026, 10, 28, 4, 0, 0, 0, time.UTC), 1, 0.75},
			{"weekend", time.Date(2026, 10, 31, 2, 0, 0, 0, time.UTC), 1, 0.75},
		} {
			t.Run(platform+"/"+tc.name, func(t *testing.T) {
				const model = "unpublished-aggregate-sku"
				group := Group{ID: 77, Platform: platform, RateMultiplier: 1}
				channel := Channel{ID: 1, Name: "Aggregate", Status: StatusActive, GroupIDs: []int64{group.ID},
					BillingModelSource: BillingModelSourceChannelMapped,
					ModelMapping:       map[string]map[string]string{platform: {model: model}}}
				svc := newPlazaServiceWithBilling([]Channel{channel}, nil, map[int64]string{group.ID: platform}, nil)
				references := NewChannelPricingReferenceService(nil, svc.billingService)
				offer := svc.catalogOffer(context.Background(), &group, &channel,
					SupportedModel{Name: model, Platform: platform}, model, svc.resolver, references, tc.at)
				require.Equal(t, "resolved", offer.PriceStatus)
				require.Equal(t, model, offer.Name)
				require.Equal(t, "token", offer.BillingUnit)
				require.NotNil(t, offer.Pricing)
				require.NotNil(t, offer.Pricing.InputPrice)
				require.NotNil(t, offer.Pricing.OutputPrice)
				require.InDelta(t, 0.15e-6, *offer.Pricing.InputPrice, 1e-15)
				require.InDelta(t, 0.60e-6, *offer.Pricing.OutputPrice, 1e-15)
				require.Equal(t, tc.factor, offer.PriceTimeMultiplier)
				require.NotNil(t, offer.TimePricing)
				require.Equal(t, "Asia/Shanghai", offer.TimePricing.Timezone)
				require.True(t, offer.TimePricing.WeekdaysOnly)
				require.Equal(t, []TimePricingPeriod{
					{StartTime: "09:00", EndTime: "12:00", Multiplier: 2},
					{StartTime: "14:00", EndTime: "18:00", Multiplier: 2},
				}, offer.TimePricing.Periods)
				ctx := WithResolvedTargetPlatform(context.Background(), platform)
				cost, err := svc.billingService.CalculateTokenCostForRequest(TokenCostRequest{
					Ctx: ctx, Model: model, Group: &group, PricingAt: tc.at, RateMultiplier: 1,
					Tokens: UsageTokens{InputTokens: 1_000_000, OutputTokens: 1_000_000}, Resolver: svc.resolver,
				})
				require.NoError(t, err)
				require.InDelta(t, tc.expectedCost, cost.TotalCost, 1e-12)
				require.InDelta(t, tc.expectedCost, cost.ActualCost, 1e-12)
			})
		}
	}
}

func TestPlatformPricingFallback_CatalogKeepsRetailTimePricingWhenDimensionsInherit(t *testing.T) {
	peak := time.Date(2026, 10, 28, 2, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		source string
		factor float64
	}{
		{PricingSourceGroup, 1},
		{PricingSourceChannel, 0.5},
	} {
		t.Run(tc.source, func(t *testing.T) {
			resolved := &ResolvedPricing{Mode: BillingModeToken, Source: tc.source,
				BasePricing: &ModelPricing{FallbackModel: "deepseek-v4-flash", FallbackPlatform: PlatformCommandCode},
				channelPricing: &ChannelModelPricing{TimePricing: &ChannelTimePricing{Timezone: "UTC", WeekdaysOnly: true,
					Periods: []ChannelTimePricingPeriod{{StartTime: "01:00", EndTime: "04:00", Multiplier: 0.5}}}}}
			require.Equal(t, tc.factor, catalogPriceTimeMultiplier(resolved, "unpublished-aggregate-sku", peak))
		})
	}
	resolved := &ResolvedPricing{Mode: BillingModeToken, Source: PricingSourcePlatformFallback,
		BasePricing: &ModelPricing{FallbackModel: "gpt-5.4", FallbackPlatform: PlatformDeepseek}}
	require.Equal(t, 1.0, catalogPriceTimeMultiplier(resolved, "deepseek-unpublished-model", peak),
		"the request name must not apply DeepSeek policy to a different fallback card")
}

func TestPlatformPricingFallback_RestrictedCatalogRecognizesExplicitGroupCardsWhenDefaultDisabled(t *testing.T) {
	const model = "unpublished-group-sku"
	zero := 0.0
	group := Group{ID: 77, Platform: PlatformOpenAI, RateMultiplier: 1, ModelPricing: []ChannelModelPricing{{
		Platform: PlatformOpenAI, Models: []string{model}, BillingMode: BillingModeToken, InputPrice: &zero, OutputPrice: &zero,
	}}}
	channel := Channel{ID: 1, Status: StatusActive, RestrictModels: true, GroupIDs: []int64{group.ID},
		BillingModelSource: BillingModelSourceChannelMapped,
		ModelMapping:       map[string]map[string]string{PlatformOpenAI: {model: model}}}
	svc := newPlazaServiceWithBilling([]Channel{channel}, nil, map[int64]string{group.ID: PlatformOpenAI}, nil)
	svc.billingService.frozenPricingFallbackModels = map[string]string{PlatformOpenAI: ""}
	groups, err := svc.ListAvailableCatalog(context.Background(), []Group{group})
	require.NoError(t, err)
	require.Len(t, groups, 1)
	require.Len(t, groups[0].Models, 1, "the caller's explicit group price supplies price evidence")
	offer := groups[0].Models[0]
	require.Equal(t, model, offer.Name)
	require.Equal(t, "resolved", offer.PriceStatus)
	require.NotNil(t, offer.Pricing)
	require.NotNil(t, offer.Pricing.InputPrice)
	require.NotNil(t, offer.Pricing.OutputPrice)
	require.Zero(t, *offer.Pricing.InputPrice)
	require.Zero(t, *offer.Pricing.OutputPrice)
}

func TestPlatformPricingFallback_CatalogPinsOneSettingsGenerationBeforeReadingChannels(t *testing.T) {
	configured := map[string]string{PlatformOpenAI: "gpt-5.4-mini"}
	channel := Channel{ID: 1, Status: StatusActive, GroupIDs: []int64{77},
		ModelMapping: map[string]map[string]string{PlatformOpenAI: {
			"first-unpublished-sku": "first-unpublished-sku", "second-unpublished-sku": "second-unpublished-sku",
		}}}
	svc := newPlazaServiceWithBilling([]Channel{channel}, nil, map[int64]string{77: PlatformOpenAI}, nil)
	reads := 0
	svc.billingService.pricingFallbackModels = func(context.Context) (map[string]string, error) {
		reads++
		return configured, nil
	}
	svc.channelRepo = &mockChannelRepository{listAllFn: func(context.Context) ([]Channel, error) {
		// The database read can overlap a settings change. It must not mutate
		// the generation already captured for this complete catalog response.
		configured[PlatformOpenAI] = "gpt-5.4"
		return []Channel{channel}, nil
	}}
	groups := []Group{{ID: 77, Platform: PlatformOpenAI, RateMultiplier: 1}}
	out, err := svc.ListAvailableCatalog(context.Background(), groups)
	require.NoError(t, err)
	require.Equal(t, 1, reads, "one response must capture fallback settings once, rather than for every probe")
	require.Len(t, out, 1)
	require.Len(t, out[0].Models, 2)
	for _, offer := range out[0].Models {
		require.Equal(t, "resolved", offer.PriceStatus)
		require.NotNil(t, offer.Pricing)
		require.NotNil(t, offer.Pricing.InputPrice)
		require.NotNil(t, offer.Pricing.OutputPrice)
		require.InDelta(t, 0.75e-6, *offer.Pricing.InputPrice, 1e-15)
		require.InDelta(t, 4.5e-6, *offer.Pricing.OutputPrice, 1e-15)
	}
	next, err := svc.ListAvailableCatalog(context.Background(), groups)
	require.NoError(t, err)
	require.Equal(t, 2, reads, "the next response captures the new configuration")
	for _, offer := range next[0].Models {
		require.NotNil(t, offer.Pricing)
		require.NotNil(t, offer.Pricing.InputPrice)
		require.NotNil(t, offer.Pricing.OutputPrice)
		require.InDelta(t, 2.5e-6, *offer.Pricing.InputPrice, 1e-15)
		require.InDelta(t, 15e-6, *offer.Pricing.OutputPrice, 1e-15)
	}
}

func TestPlatformPricingFallback_CatalogSettingsFailureDoesNotPublishAnInventedPrice(t *testing.T) {
	svc := newPlazaServiceWithBilling(nil, nil, nil, nil)
	svc.billingService.pricingFallbackModels = func(context.Context) (map[string]string, error) {
		return nil, errors.New("settings repository unavailable")
	}
	out, err := svc.ListAvailableCatalog(context.Background(), []Group{{ID: 77, Platform: PlatformOpenAI}})
	require.ErrorContains(t, err, "settings repository unavailable")
	require.Nil(t, out)
}
