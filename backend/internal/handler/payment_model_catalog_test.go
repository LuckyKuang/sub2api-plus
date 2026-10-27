//go:build unit

package handler

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	dbent "github.com/LuckyKuang/sub2api-plus/ent"
	"github.com/LuckyKuang/sub2api-plus/internal/domain"
	"github.com/LuckyKuang/sub2api-plus/internal/pkg/timezone"
	"github.com/LuckyKuang/sub2api-plus/internal/service"
	"github.com/stretchr/testify/require"
)

type saleCatalogStub struct {
	ids    []int64
	groups []service.CatalogGroup
	err    error
}

func (s *saleCatalogStub) ListSaleCatalog(_ context.Context, ids []int64) ([]service.CatalogGroup, error) {
	s.ids = ids
	return s.groups, s.err
}

func TestSaleCatalogScopesForSalePlansAndRejectsDependencyFailure(t *testing.T) {
	reader := &saleCatalogStub{groups: []service.CatalogGroup{{Group: service.Group{ID: 1}, Models: []service.CatalogOffer{}}, {Group: service.Group{ID: 99}}}}
	h := &PaymentHandler{modelCatalog: reader, userRates: &catalogAccessStub{}}
	plans := []*dbent.SubscriptionPlan{{GroupID: 1, ForSale: true}, {GroupID: 1, ForSale: true}, {GroupID: 2, ForSale: false}}
	out, err := h.saleCatalogForPlans(context.Background(), plans, 42)
	require.NoError(t, err)
	require.Equal(t, []int64{1}, reader.ids)
	require.Len(t, out, 1)
	require.Contains(t, out, int64(1))
	require.NotNil(t, out[1].Models)
	require.Nil(t, out[1].RateMin)
	require.Nil(t, out[1].RateMax)
	reader.err = errors.New("catalog unavailable")
	_, err = h.saleCatalogForPlans(context.Background(), plans, 42)
	require.Error(t, err)
}

func TestSaleModelRateSummaryUsesOnlyPublishedOffersAndRedactsInternals(t *testing.T) {
	at := time.Date(2026, 1, 5, 9, 0, 0, 0, time.UTC)
	g := service.Group{ID: 1, Platform: service.PlatformComposite, RateMultiplier: 2, BillingPolicyVersion: 9,
		BillingRatePolicy: domain.BillingRatePolicy{PlatformMultipliers: map[string]float64{service.PlatformStepFun: .5, service.PlatformOpenAI: 99}},
	}
	price := .000001
	catalog := toPlanModelCatalog(service.CatalogGroup{Group: g, AsOf: at, Models: []service.CatalogOffer{{
		PlazaModel:  service.PlazaModel{Name: "public-step", Platform: service.PlatformStepFun, Pricing: &service.ChannelModelPricing{ID: 22, ChannelID: 33, Models: []string{"private-upstream"}, InputPrice: &price}},
		BillingUnit: "token", PriceStatus: "resolved",
	}}}, nil)
	require.Equal(t, []string{service.PlatformStepFun}, catalog.Platforms)
	require.Equal(t, 1.0, *catalog.RateMin)
	require.Equal(t, 1.0, *catalog.RateMax)
	require.Equal(t, int64(9), catalog.PolicyVersion)
	require.Equal(t, at, catalog.AsOf)
	require.Equal(t, 1.0, catalog.Models[0].RateQuote.EffectiveMultiplier)
	raw, err := json.Marshal(catalog)
	require.NoError(t, err)
	for _, hidden := range []string{"private-upstream", "channel_id", "platform_multipliers", "model_multipliers"} {
		require.NotContains(t, string(raw), hidden)
	}
}

func TestSaleCatalogUsesEachBuyersPersonalRatesWithoutExistingAccess(t *testing.T) {
	group := service.Group{ID: 1, Platform: service.PlatformComposite, RateMultiplier: 2, BillingPolicyVersion: 8,
		ImageRateIndependent: true, ImageRateMultiplier: 1.5,
		BillingRatePolicy: domain.BillingRatePolicy{PlatformMultipliers: map[string]float64{service.PlatformOpenAI: 3}},
	}
	reader := &saleCatalogStub{groups: []service.CatalogGroup{{Group: group, Models: []service.CatalogOffer{
		{PlazaModel: service.PlazaModel{Name: "text-model", Platform: service.PlatformOpenAI}, BillingUnit: "token"},
		{PlazaModel: service.PlazaModel{Name: "image-model", Platform: service.PlatformOpenAI}, BillingUnit: "image"},
	}}}}
	access := &catalogAccessStub{rates: map[int64]float64{1: 0.5, 999: 99}, err: errors.New("buyer does not own this group")}
	h := &PaymentHandler{modelCatalog: reader, userRates: access}
	plans := []*dbent.SubscriptionPlan{{GroupID: 1, ForSale: true}}
	a, err := h.saleCatalogForPlans(context.Background(), plans, 42)
	require.NoError(t, err)
	require.Equal(t, []int64{42}, access.users, "only personal rates are read; existing group access is not required")
	require.Equal(t, "personal", a[1].Models[0].RateQuote.BaseSource)
	require.Equal(t, 1.5, a[1].Models[0].RateQuote.EffectiveMultiplier)
	require.Equal(t, "media", a[1].Models[1].RateQuote.BaseSource)
	require.Equal(t, 4.5, a[1].Models[1].RateQuote.EffectiveMultiplier)
	require.Equal(t, 1.5, *a[1].RateMin)
	require.Equal(t, 4.5, *a[1].RateMax)
	access.rates = map[int64]float64{1: 0}
	b, err := h.saleCatalogForPlans(context.Background(), plans, 43)
	require.NoError(t, err)
	require.Equal(t, []int64{42, 43}, access.users)
	require.Zero(t, b[1].Models[0].RateQuote.EffectiveMultiplier)
	require.Equal(t, 4.5, b[1].Models[1].RateQuote.EffectiveMultiplier)
	require.Equal(t, 1.5, a[1].Models[0].RateQuote.EffectiveMultiplier, "one buyer's projection cannot mutate another's quote")
	require.Equal(t, 2, access.rateCalls, "one rate read per catalog, not one per model")
	access.rateErr = errors.New("personal rate storage unavailable")
	failed, err := h.saleCatalogForPlans(context.Background(), plans, 44)
	require.Error(t, err)
	require.Nil(t, failed, "an unknown personal rate cannot be shown as a group-price sale quote")
}

func TestUserCatalogQuotesKeepPersonalRatesIsolated(t *testing.T) {
	g := service.Group{ID: 1, RateMultiplier: 2, BillingPolicyVersion: 4, BillingRatePolicy: domain.BillingRatePolicy{PlatformMultipliers: map[string]float64{service.PlatformStepFun: .5}}}
	cg := service.CatalogGroup{Group: g, AsOf: time.Date(2026, 1, 5, 9, 0, 0, 0, time.UTC), Models: []service.CatalogOffer{{PlazaModel: service.PlazaModel{Name: "model", Platform: service.PlatformStepFun}, BillingUnit: "token"}}}
	a := toCatalogGroup(cg, map[int64]float64{1: .2})
	b := toCatalogGroup(cg, map[int64]float64{1: 0})
	standard := toCatalogGroup(cg, nil)
	require.InDelta(t, .1, a.Models[0].RateQuote.EffectiveMultiplier, 1e-12)
	require.Equal(t, 0.0, b.Models[0].RateQuote.EffectiveMultiplier)
	require.Equal(t, 1.0, standard.Models[0].RateQuote.EffectiveMultiplier)
	require.Equal(t, "personal", a.Models[0].RateQuote.BaseSource)
	require.Equal(t, "group", standard.Models[0].RateQuote.BaseSource)
}

func TestCatalogDoesNotAdvertiseMultiplierRejectedByAdmission(t *testing.T) {
	group := service.Group{ID: 1, Platform: service.PlatformOpenAI, RateMultiplier: 2,
		BillingRatePolicy: domain.BillingRatePolicy{PlatformMultipliers: map[string]float64{service.PlatformOpenAI: domain.MaxBillingRateMultiplier}}}
	catalog := toPlanModelCatalog(service.CatalogGroup{Group: group, Models: []service.CatalogOffer{
		{PlazaModel: service.PlazaModel{Name: "gpt-5", Platform: service.PlatformOpenAI}, BillingUnit: "token"},
	}}, nil)
	require.Nil(t, catalog.Models[0].RateQuote)
	require.Nil(t, catalog.RateMin)
	require.Nil(t, catalog.RateMax)
}

func TestPlanModelCatalogPublishesPeakAndOffPeakRequestPrices(t *testing.T) {
	for _, tc := range []struct {
		name                               string
		hour, minute                       int
		peak, rate, cost, defaultModelRate float64
	}{
		{"before_peak", 9, 59, 1, 6, 12, 2},
		{"peak_start_inclusive", 10, 0, 1.5, 9, 18, 3},
		{"during_peak", 10, 30, 1.5, 9, 18, 3},
		{"peak_end_exclusive", 11, 0, 1, 6, 12, 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			at := time.Date(2026, 10, 20, tc.hour, tc.minute, 0, 0, timezone.Location())
			group := service.Group{ID: 77, Name: "Mixed subscription", Platform: service.PlatformComposite, RateMultiplier: 1, BillingPolicyVersion: 9,
				SubscriptionType: service.SubscriptionTypeSubscription, PeakRateEnabled: true, PeakStart: "10:00", PeakEnd: "11:00", PeakRateMultiplier: 1.5,
				BillingRatePolicy: domain.BillingRatePolicy{PlatformMultipliers: map[string]float64{service.PlatformOpenAI: 2}, ModelMultipliers: []domain.BillingModelRate{
					{Platform: service.PlatformOpenAI, Model: "model001", Multiplier: 3},
					{Platform: service.PlatformGrok, Model: "model001", Multiplier: 99},
				}}}
			price := 2.0
			offers := []service.CatalogOffer{}
			for _, name := range []string{"model001", "model-default"} {
				offers = append(offers, service.CatalogOffer{PlazaModel: service.PlazaModel{Name: name, Platform: service.PlatformOpenAI,
					Pricing: &service.ChannelModelPricing{BillingMode: service.BillingModePerRequest, PerRequestPrice: &price}},
					BillingMode: service.BillingModePerRequest, BillingUnit: "request", PriceStatus: "resolved", PriceTimeMultiplier: 1})
			}
			catalog := toPlanModelCatalog(service.CatalogGroup{Group: group, Models: offers, AsOf: at}, nil)
			require.Equal(t, at, catalog.AsOf)
			require.Equal(t, int64(9), catalog.PolicyVersion)
			require.Equal(t, []string{service.PlatformOpenAI}, catalog.Platforms)
			require.Len(t, catalog.Models, 2)
			model := catalog.Models[0]
			require.NotNil(t, model.RateQuote)
			quote := model.RateQuote
			require.Equal(t, "model001", quote.Model)
			require.Equal(t, service.PlatformOpenAI, quote.Platform)
			require.Equal(t, "request", quote.BillingUnit)
			require.Equal(t, 1.0, quote.BaseMultiplier)
			require.Equal(t, "group", quote.BaseSource, "a buyer without a personal override uses the group base")
			require.Equal(t, 2.0, quote.PlatformMultiplier)
			require.Equal(t, 3.0, quote.ModelMultiplier)
			require.Equal(t, 6.0, quote.PolicyMultiplier)
			require.Equal(t, tc.peak, quote.PeakMultiplier)
			require.Equal(t, service.BillingPeakScopeAllMeters, quote.PeakScope)
			require.Equal(t, service.BillingRateCalculationProduct, quote.RateCalculation)
			require.Equal(t, tc.rate, quote.EffectiveMultiplier)
			require.Equal(t, tc.cost, *model.Pricing.PerRequestPrice*quote.EffectiveMultiplier, "$2 ×platform2 ×model3 ×applicable peak")
			require.Equal(t, at, quote.AsOf)
			defaultQuote := catalog.Models[1].RateQuote
			require.NotNil(t, defaultQuote)
			require.Equal(t, 1.0, defaultQuote.ModelMultiplier)
			require.Equal(t, tc.defaultModelRate, defaultQuote.EffectiveMultiplier)
			require.Equal(t, tc.defaultModelRate, *catalog.RateMin)
			require.Equal(t, tc.rate, *catalog.RateMax)
			info := planGroupInfoFromCatalog(catalog)
			require.True(t, info.PeakRateEnabled)
			require.Equal(t, "10:00", info.PeakStart)
			require.Equal(t, "11:00", info.PeakEnd)
			require.Equal(t, 1.5, info.PeakRateMultiplier)
			raw, err := json.Marshal(catalog)
			require.NoError(t, err)
			require.Contains(t, string(raw), `"peak_scope":"all_meters"`)
			require.NotContains(t, string(raw), "platform_multipliers")
		})
	}
}

func TestPlanModelCatalogPeakEligibilityAndBuyerPersonalBase(t *testing.T) {
	for _, tc := range []struct {
		name, subscriptionType string
		enabled                bool
		personal               *float64
		peak, base, cost       float64
	}{
		{"subscription_personal_base", service.SubscriptionTypeSubscription, true, paymentCatalogRatePtr(.5), 1.5, .5, 9},
		{"disabled_peak", service.SubscriptionTypeSubscription, false, nil, 1, 1, 12},
		{"standard_group", service.SubscriptionTypeStandard, true, nil, 1, 1, 12},
	} {
		t.Run(tc.name, func(t *testing.T) {
			at := time.Date(2026, 10, 20, 10, 30, 0, 0, timezone.Location())
			group := service.Group{ID: 77, Platform: service.PlatformComposite, RateMultiplier: 1, SubscriptionType: tc.subscriptionType,
				PeakRateEnabled: tc.enabled, PeakStart: "10:00", PeakEnd: "11:00", PeakRateMultiplier: 1.5,
				BillingRatePolicy: domain.BillingRatePolicy{PlatformMultipliers: map[string]float64{service.PlatformOpenAI: 2}, ModelMultipliers: []domain.BillingModelRate{{Platform: service.PlatformOpenAI, Model: "model001", Multiplier: 3}}}}
			var userRates map[int64]float64
			if tc.personal != nil {
				userRates = map[int64]float64{77: *tc.personal}
			}
			price := 2.0
			catalog := toPlanModelCatalog(service.CatalogGroup{Group: group, AsOf: at, Models: []service.CatalogOffer{{
				PlazaModel:  service.PlazaModel{Name: "model001", Platform: service.PlatformOpenAI, Pricing: &service.ChannelModelPricing{BillingMode: service.BillingModePerRequest, PerRequestPrice: &price}},
				BillingMode: service.BillingModePerRequest, BillingUnit: "request", PriceStatus: "resolved",
			}}}, userRates)
			quote := catalog.Models[0].RateQuote
			require.NotNil(t, quote)
			require.Equal(t, tc.base, quote.BaseMultiplier)
			require.Equal(t, tc.peak, quote.PeakMultiplier)
			require.Equal(t, tc.cost, *catalog.Models[0].Pricing.PerRequestPrice*quote.EffectiveMultiplier)
		})
	}
}

func paymentCatalogRatePtr(value float64) *float64 { return &value }
