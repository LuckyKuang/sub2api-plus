//go:build unit

package handler

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/domain"
	"github.com/LuckyKuang/sub2api-plus/internal/service"
	"github.com/stretchr/testify/require"
)

func catalogRateFixture() service.CatalogGroup {
	return service.CatalogGroup{Group: service.Group{ID: 7, Platform: service.PlatformComposite, RateMultiplier: 2, BillingPolicyVersion: 9,
		ImageRateIndependent: true, ImageRateMultiplier: .25,
		BillingRatePolicy: domain.BillingRatePolicy{PlatformMultipliers: map[string]float64{service.PlatformOpenAI: 1.2, service.PlatformStepFun: .5, service.PlatformGrok: 99},
			ModelMultipliers: []domain.BillingModelRate{{Platform: service.PlatformOpenAI, Model: "public-priced", Multiplier: 1.5}, {Platform: service.PlatformOpenAI, Model: "public-free", Multiplier: 0}}}},
		AsOf: time.Date(2026, 10, 20, 0, 0, 0, 0, time.UTC), Models: []service.CatalogOffer{
			{PlazaModel: service.PlazaModel{Name: "public-default", Platform: service.PlatformOpenAI}, BillingUnit: "token"},
			{PlazaModel: service.PlazaModel{Name: "public-priced", Platform: service.PlatformOpenAI}, BillingUnit: "token"},
			{PlazaModel: service.PlazaModel{Name: "public-free", Platform: service.PlatformOpenAI}, BillingUnit: "token"},
			{PlazaModel: service.PlazaModel{Name: "public-priced", Platform: service.PlatformOpenAI}, BillingUnit: "image"},
			{PlazaModel: service.PlazaModel{Name: "public-priced", Platform: service.PlatformStepFun}, BillingUnit: "token"},
		}}
}

func TestCatalogRateSummaryUsesActualSupplierUnitsAndIndependentProductFactors(t *testing.T) {
	cg := catalogRateFixture()
	got := toCatalogGroup(cg, map[int64]float64{7: .5})
	require.Equal(t, "available", got.RateSummary.Status)
	require.Equal(t, cg.AsOf, got.RateSummary.AsOf)
	require.EqualValues(t, 9, got.RateSummary.PolicyVersion)
	require.Len(t, got.RateSummary.Entries, 3, "an unused Grok rate does not advertise Grok support")
	image, token, step := got.RateSummary.Entries[0], got.RateSummary.Entries[1], got.RateSummary.Entries[2]
	require.Equal(t, "openai", image.Platform)
	require.Equal(t, "image", image.BillingUnit)
	require.Equal(t, 1, image.ModelCount)
	require.True(t, image.HasModelExceptions)
	require.NotNil(t, image.DefaultQuote)
	require.InDelta(t, .3, image.DefaultQuote.EffectiveMultiplier, 1e-12)
	require.InDelta(t, .45, *image.RateMin, 1e-12)
	require.InDelta(t, .45, *image.RateMax, 1e-12)
	require.Equal(t, "media", image.DefaultQuote.BaseSource)
	require.Equal(t, "token", token.BillingUnit)
	require.Equal(t, 3, token.ModelCount)
	require.True(t, token.HasModelExceptions)
	require.InDelta(t, .6, token.DefaultQuote.EffectiveMultiplier, 1e-12)
	require.Equal(t, 1.0, token.DefaultQuote.ModelMultiplier)
	require.Empty(t, token.DefaultQuote.Model)
	require.Zero(t, *token.RateMin)
	require.InDelta(t, .9, *token.RateMax, 1e-12)
	require.Equal(t, "stepfun", step.Platform)
	require.False(t, step.HasModelExceptions, "same public model on another supplier cannot inherit the OpenAI exception")
	require.InDelta(t, .25, *step.RateMin, 1e-12)
	require.InDelta(t, .25, *step.RateMax, 1e-12)
	require.Equal(t, service.BillingRateCalculationProduct, got.Models[1].RateQuote.RateCalculation)
	require.Equal(t, 1.2, got.Models[1].RateQuote.PlatformMultiplier)
	require.Equal(t, 1.5, got.Models[1].RateQuote.ModelMultiplier)
	raw, err := json.Marshal(got)
	require.NoError(t, err)
	for _, hidden := range []string{"platform_multipliers", "model_multipliers", "credentials", "account_id", "channel_id"} {
		require.NotContains(t, string(raw), hidden)
	}
}

func TestCatalogRateSummaryRetainsPersonalZeroAndDoesNotMutateOtherQuotes(t *testing.T) {
	cg := catalogRateFixture()
	paid := toCatalogGroup(cg, map[int64]float64{7: .5})
	free := toCatalogGroup(cg, map[int64]float64{7: 0})
	require.Zero(t, free.RateSummary.Entries[1].DefaultQuote.EffectiveMultiplier)
	require.Zero(t, *free.RateSummary.Entries[1].RateMin)
	require.Zero(t, *free.RateSummary.Entries[1].RateMax)
	require.InDelta(t, .3, free.RateSummary.Entries[0].DefaultQuote.EffectiveMultiplier, 1e-12, "independent media base keeps its contract")
	require.InDelta(t, .9, *paid.RateSummary.Entries[1].RateMax, 1e-12)
	empty := toCatalogGroup(service.CatalogGroup{Group: cg.Group, Models: []service.CatalogOffer{}}, nil)
	require.Equal(t, "empty", empty.RateSummary.Status)
	require.NotNil(t, empty.RateSummary.Entries)
	require.Empty(t, empty.RateSummary.Entries)
}

func TestCatalogInvalidPolicyCannotAdvertiseDefaultOne(t *testing.T) {
	cg := catalogRateFixture()
	cg.Group.BillingRatePolicy.ModelMultipliers[0].Multiplier = -1
	got := toCatalogGroup(cg, map[int64]float64{7: .5})
	require.Equal(t, "unavailable", got.RateSummary.Status)
	for _, model := range got.Models {
		require.Nil(t, model.RateQuote)
	}
	for _, entry := range got.RateSummary.Entries {
		require.Nil(t, entry.DefaultQuote)
		require.Nil(t, entry.RateMin)
		require.Nil(t, entry.RateMax)
	}
}

type catalogRateReader struct{ group service.CatalogGroup }

func (r *catalogRateReader) ListAvailableCatalog(context.Context, []service.Group) ([]service.CatalogGroup, error) {
	return []service.CatalogGroup{r.group}, nil
}

func TestAvailableCatalogUnknownPersonalRatesSuppressAllPublishedRateQuotes(t *testing.T) {
	cg := catalogRateFixture()
	access := &catalogAccessStub{groups: []service.Group{cg.Group}, rates: map[int64]float64{7: 9}, rateErr: errors.New("rate store unavailable")}
	h := &AvailableChannelHandler{apiKeyService: access, plazaService: &catalogRateReader{group: cg}}
	c, recorder := catalogContext("?view=catalog", true)
	h.listCatalog(c, 42)
	require.Equal(t, 200, recorder.Code)
	var body struct {
		Data channelCatalog `json:"data"`
	}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &body))
	require.Equal(t, "unavailable", body.Data.UserRateStatus)
	require.Len(t, body.Data.Groups, 1)
	got := body.Data.Groups[0]
	require.Nil(t, got.UserRateMultiplier)
	require.Equal(t, "unavailable", got.RateSummary.Status)
	require.Len(t, got.RateSummary.Entries, 3)
	for _, offer := range got.Models {
		require.Nil(t, offer.RateQuote)
	}
	for _, entry := range got.RateSummary.Entries {
		require.Nil(t, entry.DefaultQuote)
		require.Nil(t, entry.RateMin)
		require.Nil(t, entry.RateMax)
	}
}

// Key group choices carry commercial quotes independently of the optional
// catalog page. Authentication and bindable-group permissions still apply.
func TestKeyGroupQuotesRemainAvailableWhenCatalogPageDisabled(t *testing.T) {
	cg := catalogRateFixture()
	access := &catalogAccessStub{groups: []service.Group{cg.Group}, rates: map[int64]float64{7: .5}}
	h := &AvailableChannelHandler{apiKeyService: access, plazaService: &catalogRateReader{group: cg}}
	c, w := catalogContext("?view=key_quotes", false)
	h.List(c)
	require.Equal(t, 401, w.Code)
	c, w = catalogContext("?view=catalog", true)
	h.List(c)
	require.Contains(t, w.Body.String(), `"groups":[]`)
	c, w = catalogContext("?view=key_quotes", true)
	h.List(c)
	require.Equal(t, 200, w.Code)
	var got struct {
		Data channelCatalog `json:"data"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &got))
	require.Len(t, got.Data.Groups, 1)
	require.EqualValues(t, 7, got.Data.Groups[0].ID)
	require.InDelta(t, .6, got.Data.Groups[0].RateSummary.Entries[1].DefaultQuote.EffectiveMultiplier, 1e-12)
	for _, bad := range []string{"?view=key_quotes&view=catalog", "?view=key_quotes&view=key_quotes"} {
		c, w = catalogContext(bad, true)
		h.List(c)
		require.JSONEq(t, `{"code":0,"message":"success","data":[]}`, w.Body.String())
	}
}
