//go:build unit

package service

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestPlatformPricingFallback_DeepSeekProviderSKUsKeepPeakOffPeakAndProvenance(t *testing.T) {
	for _, sku := range []struct {
		model                                    string
		input, output, cacheRead, standard, peak float64
	}{
		{"deepseek/deepseek-v4-flash", 0.15e-6, 0.60e-6, 0.003e-6, 0.000453, 0.000906},
		{"deepseek/deepseek-flash", 0.15e-6, 0.60e-6, 0.003e-6, 0.000453, 0.000906},
		{"deepseek/deepseek-v4.1-flash", 0.15e-6, 0.60e-6, 0.003e-6, 0.000453, 0.000906},
		{"deepseek/deepseek-v4-flash-0731", 0.15e-6, 0.60e-6, 0.003e-6, 0.000453, 0.000906},
		{"deepseek/deepseek-v4-flash-vision-exp", 0.15e-6, 0.60e-6, 0.003e-6, 0.000453, 0.000906},
		{"deepseek/deepseek-v4-pro", 0.66e-6, 1.98e-6, 0.022e-6, 0.001672, 0.003344},
		{"deepseek/deepseek-v4-pro-0813", 0.66e-6, 1.98e-6, 0.022e-6, 0.001672, 0.003344},
	} {
		for _, period := range []struct {
			name string
			at   time.Time
			peak bool
		}{
			{"weekday peak", time.Date(2026, 10, 26, 2, 0, 0, 0, time.UTC), true},
			{"weekday off peak", time.Date(2026, 10, 26, 4, 0, 0, 0, time.UTC), false},
			{"weekend", time.Date(2026, 10, 25, 2, 0, 0, 0, time.UTC), false},
		} {
			for _, platform := range []string{PlatformDeepseek, PlatformCommandCode, PlatformCline} {
				t.Run(platform+"/"+sku.model+"/"+period.name, func(t *testing.T) {
					billing := platformFallbackTestBilling(nil)
					billing.frozenPricingAt = period.at
					billing.frozenPricingFallbackModels = map[string]string{platform: ""}
					model := sku.model
					if platform != PlatformDeepseek {
						model = "unpublished-aggregate-sku"
						billing.frozenPricingFallbackModels[platform] = sku.model
					}
					price, err := billing.GetModelPricingForPlatform(context.Background(), platform, model)
					require.NoError(t, err)
					requirePlatformFallbackRates(t, price, sku.input, sku.output)
					require.InDelta(t, sku.cacheRead, price.CacheReadPricePerToken, 1e-15)
					if platform == PlatformDeepseek {
						require.Empty(t, price.FallbackModel)
						require.Empty(t, price.FallbackPlatform)
					} else {
						require.Equal(t, sku.model, price.FallbackModel)
						require.Equal(t, platform, price.FallbackPlatform)
					}
					group := &Group{ID: 77, Platform: platform, RateMultiplier: 1}
					cost, err := billing.CalculateTokenCostForRequest(TokenCostRequest{
						Ctx: context.Background(), Model: model, Group: group, PricingAt: period.at, RateMultiplier: 1,
						Tokens: UsageTokens{InputTokens: 1000, OutputTokens: 500, CacheReadTokens: 1000},
					})
					require.NoError(t, err)
					expected := sku.standard
					if period.peak {
						expected = sku.peak
					}
					require.InDelta(t, expected, cost.TotalCost, 1e-12)
					require.InDelta(t, expected, cost.ActualCost, 1e-12)
				})
			}
		}
	}
}

func TestPlatformPricingFallback_DeepSeekProviderCatalogSeparatesStandardCardAndTimeFactor(t *testing.T) {
	const model = "unpublished-aggregate-sku"
	for _, tc := range []struct {
		fallback      string
		input, output float64
	}{
		{"deepseek/deepseek-v4-flash-0731", 0.15e-6, 0.60e-6},
		{"deepseek/deepseek-v4-pro-0813", 0.66e-6, 1.98e-6},
	} {
		for _, period := range []struct {
			name   string
			at     time.Time
			factor float64
		}{
			{"weekday peak", time.Date(2026, 10, 26, 2, 0, 0, 0, time.UTC), 2},
			{"weekday off peak", time.Date(2026, 10, 26, 4, 0, 0, 0, time.UTC), 1},
			{"weekend", time.Date(2026, 10, 25, 2, 0, 0, 0, time.UTC), 1},
		} {
			t.Run(tc.fallback+"/"+period.name, func(t *testing.T) {
				group := Group{ID: 77, Platform: PlatformCommandCode, RateMultiplier: 1}
				channel := Channel{ID: 1, Status: StatusActive, GroupIDs: []int64{group.ID}, BillingModelSource: BillingModelSourceRequested}
				svc := newPlazaServiceWithBilling([]Channel{channel}, nil, map[int64]string{group.ID: PlatformCommandCode}, nil)
				svc.billingService.frozenPricingFallbackModels = map[string]string{PlatformCommandCode: tc.fallback}
				references := NewChannelPricingReferenceService(nil, svc.billingService)
				offer := svc.catalogOffer(context.Background(), &group, &channel,
					SupportedModel{Name: model, Platform: PlatformCommandCode}, model, svc.resolver, references, period.at)
				require.Equal(t, "resolved", offer.PriceStatus)
				require.Equal(t, model, offer.Name)
				require.Equal(t, "token", offer.BillingUnit)
				require.NotNil(t, offer.Pricing)
				require.NotNil(t, offer.Pricing.InputPrice)
				require.NotNil(t, offer.Pricing.OutputPrice)
				require.InDelta(t, tc.input, *offer.Pricing.InputPrice, 1e-15)
				require.InDelta(t, tc.output, *offer.Pricing.OutputPrice, 1e-15)
				require.Equal(t, period.factor, offer.PriceTimeMultiplier)
				require.NotNil(t, offer.TimePricing)
				require.Equal(t, "Asia/Shanghai", offer.TimePricing.Timezone)
				require.True(t, offer.TimePricing.WeekdaysOnly)
				require.Equal(t, []TimePricingPeriod{
					{StartTime: "09:00", EndTime: "12:00", Multiplier: 2},
					{StartTime: "14:00", EndTime: "18:00", Multiplier: 2},
				}, offer.TimePricing.Periods)
			})
		}
	}
}

func TestPlatformPricingFallback_UnknownDeepSeekProviderSKUsKeepTheirExplicitCard(t *testing.T) {
	const model = "deepseek/deepseek-v4-flash-preview"
	catalog := &PricingService{pricingData: map[string]*LiteLLMModelPricing{
		model: {InputCostPerToken: 7e-6, OutputCostPerToken: 11e-6},
	}}
	billing := platformFallbackTestBilling(catalog)
	billing.frozenPricingFallbackModels = map[string]string{PlatformCommandCode: model}
	price, err := billing.GetModelPricingForPlatform(context.Background(), PlatformCommandCode, "unpublished-aggregate-sku")
	require.NoError(t, err)
	requirePlatformFallbackRates(t, price, 7e-6, 11e-6)
	require.Equal(t, model, price.FallbackModel)
	peak := time.Date(2026, 10, 26, 2, 0, 0, 0, time.UTC)
	cost, err := billing.CalculateTokenCostForRequest(TokenCostRequest{
		Ctx: context.Background(), Model: "unpublished-aggregate-sku",
		Group: &Group{ID: 77, Platform: PlatformCommandCode}, RateMultiplier: 1, PricingAt: peak,
		Tokens: UsageTokens{InputTokens: 1000, OutputTokens: 1000},
	})
	require.NoError(t, err)
	require.InDelta(t, 0.018, cost.TotalCost, 1e-12,
		"a literal unknown SKU card must not acquire another DeepSeek SKU's official rates or peak rule")
	require.False(t, isDeepSeekModel("deepseek/deepseek-v4-flash"),
		"pricing-only aliases must not expand protocol/outbound classification")
}
