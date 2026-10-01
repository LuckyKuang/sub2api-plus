//go:build unit

package service

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAvailableCatalogAuthorizationAndStableIdentity(t *testing.T) {
	groups := []Group{{ID: 1, Name: "Zulu", Platform: PlatformOpenAI}, {ID: 2, Name: "Alpha", Platform: PlatformComposite}, {ID: 3, Name: "Empty", Platform: PlatformOpenAI}, {ID: 4, Name: "Inactive channel", Platform: PlatformOpenAI}}
	ch := plazaPricedChannel(11, "Visible", []int64{1, 2, 999}, PlatformOpenAI, "gpt-6-sol")
	ch.ModelPricing = append(ch.ModelPricing, ChannelModelPricing{Platform: PlatformAnthropic, Models: []string{"claude-sonnet-4"}, InputPrice: testPtrFloat64(0)})
	inactive := plazaPricedChannel(12, "Hidden", []int64{4}, PlatformOpenAI, "hidden")
	inactive.Status = StatusDisabled
	svc := newPlazaServiceWithBilling([]Channel{ch, inactive}, nil, nil, nil)
	out, err := svc.ListAvailableCatalog(context.Background(), groups)
	require.NoError(t, err)
	require.Len(t, out, 4)
	require.Equal(t, "Alpha", out[0].Group.Name)
	require.Len(t, out[0].Models, 2)
	require.Empty(t, out[1].Models)
	require.Empty(t, out[2].Models)
	require.Len(t, out[3].Models, 1)
	var compositeKey string
	for _, m := range out[0].Models {
		if m.Name == "gpt-6-sol" {
			compositeKey = m.OfferKey
		}
	}
	require.NotEqual(t, compositeKey, out[3].Models[0].OfferKey)
	require.Equal(t, "Visible", out[3].Models[0].Source.Name)
	key := out[3].Models[0].OfferKey
	groups[0].Name = "Renamed"
	ch.Name = "Renamed channel"
	ch.ModelPricing[0].InputPrice = testPtrFloat64(9e-6)
	updated := newPlazaServiceWithBilling([]Channel{ch}, nil, nil, nil)
	again, err := updated.ListAvailableCatalog(context.Background(), groups[:1])
	require.NoError(t, err)
	require.Equal(t, key, again[0].Models[0].OfferKey)
	require.InDelta(t, 9e-6, *again[0].Models[0].Pricing.InputPrice, 1e-12)
}

func TestAvailableCatalogMappingSourcesAndZero(t *testing.T) {
	for _, source := range []string{BillingModelSourceRequested, BillingModelSourceChannelMapped, BillingModelSourceUpstream, BillingModelSourceResponse} {
		t.Run(source, func(t *testing.T) {
			group := Group{ID: 1, Platform: PlatformOpenAI, LongContextPricingEnabled: true}
			ch := plazaPricedChannel(1, "Channel", []int64{1}, PlatformOpenAI, "target")
			ch.BillingModelSource = source
			ch.ModelMapping = map[string]map[string]string{PlatformOpenAI: {"alias": "target"}}
			ch.ModelPricing = append(ch.ModelPricing, ChannelModelPricing{Platform: PlatformOpenAI, Models: []string{"alias"}, InputPrice: testPtrFloat64(0), OutputPrice: testPtrFloat64(0)})
			svc := newPlazaServiceWithBilling([]Channel{ch}, nil, nil, nil)
			out, err := svc.ListAvailableCatalog(context.Background(), []Group{group})
			require.NoError(t, err)
			offer := out[0].Models[0]
			require.Equal(t, "alias", offer.Name)
			if source == BillingModelSourceUpstream || source == BillingModelSourceResponse {
				require.Equal(t, "request_dependent", offer.PriceReason)
				require.Nil(t, offer.Pricing)
			} else {
				require.Equal(t, "resolved", offer.PriceStatus)
				want := 3e-6
				if source == BillingModelSourceRequested {
					want = 0
				}
				require.InDelta(t, want, *offer.Pricing.InputPrice, 1e-12)
			}
		})
	}
}

func TestAvailableCatalogBillingParity(t *testing.T) {
	group := Group{ID: 1, Platform: PlatformOpenAI, LongContextPricingEnabled: true}
	ch := plazaPricedChannel(1, "Channel", []int64{1}, PlatformOpenAI, "gpt-6-sol")
	ch.ModelPricing[0].CacheWrite1hPrice = testPtrFloat64(8e-6)
	ch.ModelPricing[0].FastMultiplier = testPtrFloat64(3)
	ch.ModelPricing[0].FlexMultiplier = testPtrFloat64(0.25)
	ch.ModelPricing[0].ReasoningEffortMultipliers = map[string]float64{"high": 1.2}
	ch.ModelPricing[0].Intervals = []PricingInterval{{MinTokens: 100, InputMultiplier: testPtrFloat64(2), OutputPrice: testPtrFloat64(20e-6)}}
	ch.ModelPricing[0].TimePricing = &ChannelTimePricing{Timezone: "Europe/Amsterdam", WeekdaysOnly: true, Periods: []ChannelTimePricingPeriod{{StartTime: "10:00:01", EndTime: "12:00:02", Multiplier: 2}}}
	svc := newPlazaServiceWithBilling([]Channel{ch}, nil, map[int64]string{1: PlatformOpenAI}, nil)
	out, err := svc.ListAvailableCatalog(context.Background(), []Group{group})
	require.NoError(t, err)
	o := out[0].Models[0]
	require.Equal(t, ContextPricingBasisWholeRequest, o.LongContextBasis)
	require.Equal(t, "Europe/Amsterdam", o.TimePricing.Timezone)
	require.Equal(t, "10:00:01", o.TimePricing.Periods[0].StartTime)
	require.Equal(t, 1.2, o.Pricing.ReasoningEffortMultipliers["high"])
	for _, tier := range append([]CatalogServiceTier{{Name: "", Pricing: o.Pricing}}, o.ServiceTiers...) {
		for _, n := range []int{99, 100, 101} {
			cost, err := svc.billingService.CalculateTokenCostForRequest(TokenCostRequest{Ctx: context.Background(), Model: o.Name, Group: &group, Tokens: UsageTokens{InputTokens: n}, RateMultiplier: 1, ServiceTier: tier.Name, Resolver: svc.resolver})
			require.NoError(t, err)
			price := tier.Pricing.InputPrice
			if n > 100 {
				price = tier.Pricing.Intervals[1].InputPrice
			}
			require.InDelta(t, cost.ActualCost, *price*float64(n), 1e-10)
		}
	}
	group.LongContextPricingEnabled = false
	flat, err := svc.ListAvailableCatalog(context.Background(), []Group{group})
	require.NoError(t, err)
	require.Empty(t, flat[0].Models[0].Pricing.Intervals)
}

func TestAvailableCatalogMediaUnitsAndGroupOverrides(t *testing.T) {
	group := Group{ID: 1, Platform: PlatformGrok, ImagePrice2K: testPtrFloat64(0.7), VideoPrice720P: testPtrFloat64(0.4)}
	ch := Channel{ID: 1, Status: StatusActive, GroupIDs: []int64{1}, ModelPricing: []ChannelModelPricing{
		{Platform: PlatformGrok, Models: []string{"grok-imagine-image"}, BillingMode: BillingModeImage, PerRequestPrice: testPtrFloat64(0.1)},
		{Platform: PlatformGrok, Models: []string{"grok-imagine-video"}, BillingMode: BillingModeVideo, PerRequestPrice: testPtrFloat64(0.2)},
		{Platform: PlatformGrok, Models: []string{"request-model"}, BillingMode: BillingModePerRequest, PerRequestPrice: testPtrFloat64(0)},
	}}
	svc := newPlazaServiceWithBilling([]Channel{ch}, nil, nil, nil)
	out, err := svc.ListAvailableCatalog(context.Background(), []Group{group})
	require.NoError(t, err)
	require.Equal(t, "image", out[0].Models[0].BillingUnit)
	require.InDelta(t, 0.7, *out[0].Models[0].MediaTiers[1].Price, 1e-12)
	require.Equal(t, "second", out[0].Models[1].BillingUnit)
	require.InDelta(t, 0.4, *out[0].Models[1].MediaTiers[1].Price, 1e-12)
	require.Equal(t, "request", out[0].Models[2].BillingUnit)
	require.Equal(t, 0.0, *out[0].Models[2].Pricing.PerRequestPrice)
}

func TestAvailableCatalogFailuresAndNoAuthorizedGroups(t *testing.T) {
	svc := &ModelPlazaService{channelRepo: &mockChannelRepository{listAllFn: func(context.Context) ([]Channel, error) { return nil, errors.New("read failed") }}}
	empty, err := svc.ListAvailableCatalog(context.Background(), nil)
	require.NoError(t, err)
	require.NotNil(t, empty)
	_, err = svc.ListAvailableCatalog(context.Background(), []Group{{ID: 1}})
	require.Error(t, err)
}

func TestAvailableCatalogLegacyMediaCardsAreRequestDependent(t *testing.T) {
	for _, model := range []string{"gpt-image-2", "grok-imagine-image", "grok-imagine-video", "gemini-3.1-flash-image"} {
		t.Run(model, func(t *testing.T) {
			group := Group{ID: 1, Platform: PlatformOpenAI, ImagePrice2K: testPtrFloat64(0.7), VideoPrice720P: testPtrFloat64(0.4)}
			ch := plazaPricedChannel(1, "Media", []int64{1}, PlatformOpenAI, model)
			ch.ModelPricing[0].BillingMode = BillingModePerRequest
			ch.ModelPricing[0].PerRequestPrice = testPtrFloat64(0.1)
			svc := newPlazaServiceWithBilling([]Channel{ch}, nil, nil, nil)
			out, err := svc.ListAvailableCatalog(context.Background(), []Group{group})
			require.NoError(t, err)
			m := out[0].Models[0]
			require.Equal(t, "unknown", m.PriceStatus)
			require.Equal(t, "unknown", m.BillingUnit)
			require.Equal(t, "request_dependent", m.PriceReason)
			require.Nil(t, m.Pricing)
			ch.ModelPricing[0].BillingMode = BillingModeToken
			ch.ModelPricing[0].PerRequestPrice = nil
			svc = newPlazaServiceWithBilling([]Channel{ch}, nil, nil, nil)
			out, err = svc.ListAvailableCatalog(context.Background(), []Group{group})
			require.NoError(t, err)
			require.Equal(t, "token", out[0].Models[0].BillingUnit)
			require.Equal(t, "resolved", out[0].Models[0].PriceStatus)
		})
	}
}

func TestAvailableCatalogMediaFallbackAndUnsupportedUnit(t *testing.T) {
	group := Group{ID: 1, Platform: PlatformComposite}
	ch := Channel{ID: 1, Status: StatusActive, GroupIDs: []int64{1}, ModelMapping: map[string]map[string]string{PlatformGrok: {"grok-imagine-video": "grok-imagine-video"}, PlatformOpenAI: {"gpt-4o-mini-tts": "gpt-4o-mini-tts"}}}
	catalog := mustCatalogFromJSON(`{"gpt-4o-mini-tts":{"litellm_provider":"openai","mode":"audio_speech","input_cost_per_token":0.000001}}`)
	svc := newPlazaServiceWithBilling([]Channel{ch}, nil, nil, catalog)
	out, err := svc.ListAvailableCatalog(context.Background(), []Group{group})
	require.NoError(t, err)
	require.Len(t, out[0].Models, 2)
	for _, m := range out[0].Models {
		if m.Platform == PlatformGrok {
			require.Equal(t, "resolved", m.PriceStatus)
			require.Equal(t, "second", m.BillingUnit)
			expected := svc.billingService.CalculateVideoCost(m.Name, "480p", 1, 1, nil, 1)
			require.InDelta(t, expected.ActualCost, *m.Pricing.PerRequestPrice, 1e-12)
		} else {
			require.Equal(t, "unsupported_unit", m.PriceReason)
			require.Nil(t, m.Pricing)
		}
	}
}

func TestAvailableCatalogDeepSeekStandardAndDerivedPeriods(t *testing.T) {
	group := Group{ID: 1, Platform: PlatformDeepseek}
	ch := Channel{ID: 1, Status: StatusActive, GroupIDs: []int64{1}, ModelMapping: map[string]map[string]string{PlatformDeepseek: {"deepseek-flash": "deepseek-flash"}}}
	catalog := mustCatalogFromJSON(`{"deepseek-flash":{"litellm_provider":"deepseek","mode":"chat","input_cost_per_token":0.000001,"output_cost_per_token":0.000002}}`)
	svc := newPlazaServiceWithBilling([]Channel{ch}, nil, nil, catalog)
	out, err := svc.ListAvailableCatalog(context.Background(), []Group{group})
	require.NoError(t, err)
	m := out[0].Models[0]
	require.Equal(t, "resolved", m.PriceStatus)
	require.InDelta(t, deepseekFlashOffPeakInputPrice, *m.Pricing.InputPrice, 1e-12)
	require.NotNil(t, m.TimePricing)
	require.True(t, m.TimePricing.WeekdaysOnly)
	require.Equal(t, "Asia/Shanghai", m.TimePricing.Timezone)
	require.Equal(t, []TimePricingPeriod{{StartTime: "09:00", EndTime: "12:00", Multiplier: 2}, {StartTime: "14:00", EndTime: "18:00", Multiplier: 2}}, m.TimePricing.Periods)
}

func TestAvailableCatalogImageTokenPricesFollowBillingForEveryServiceTier(t *testing.T) {
	for _, imagePrice := range []float64{0, 8e-6} {
		t.Run(fmt.Sprint(imagePrice), func(t *testing.T) {
			group := Group{ID: 1, Platform: PlatformOpenAI}
			ch := plazaPricedChannel(1, "Image tokens", []int64{1}, PlatformOpenAI, "custom-image-token-model")
			ch.ModelPricing[0].ImageInputPrice = testPtrFloat64(imagePrice)
			ch.ModelPricing[0].ImageOutputPrice = testPtrFloat64(imagePrice)
			ch.ModelPricing[0].PerRequestPrice = testPtrFloat64(99) // ignored by token billing
			ch.ModelPricing[0].FastMultiplier = testPtrFloat64(3)
			ch.ModelPricing[0].FlexMultiplier = testPtrFloat64(0.25)
			svc := newPlazaServiceWithBilling([]Channel{ch}, nil, map[int64]string{1: PlatformOpenAI}, nil)
			out, err := svc.ListAvailableCatalog(context.Background(), []Group{group})
			require.NoError(t, err)
			offer := out[0].Models[0]
			require.Equal(t, "resolved", offer.PriceStatus)
			require.Len(t, offer.ServiceTiers, 2)
			for _, tier := range append([]CatalogServiceTier{{Pricing: offer.Pricing}}, offer.ServiceTiers...) {
				require.Nil(t, tier.Pricing.PerRequestPrice)
				require.NotNil(t, tier.Pricing.ImageInputPrice)
				require.NotNil(t, tier.Pricing.ImageOutputPrice)
				cost, err := svc.billingService.CalculateTokenCostForRequest(TokenCostRequest{
					Ctx: context.Background(), Model: offer.Name, Group: &group,
					Tokens:         UsageTokens{InputTokens: 100, ImageInputTokens: 100, OutputTokens: 50, ImageOutputTokens: 50},
					RateMultiplier: 1, ServiceTier: tier.Name, Resolver: svc.resolver,
				})
				require.NoError(t, err)
				require.InDelta(t, cost.ActualCost, *tier.Pricing.ImageInputPrice*100+*tier.Pricing.ImageOutputPrice*50, 1e-12)
				if imagePrice == 0 {
					require.Greater(t, *tier.Pricing.ImageInputPrice, 0.0, "zero input falls back to text input")
					require.Zero(t, *tier.Pricing.ImageOutputPrice, "explicit zero output stays free")
				}
			}
		})
	}
}
