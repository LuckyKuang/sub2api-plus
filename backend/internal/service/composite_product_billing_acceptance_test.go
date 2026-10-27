//go:build unit

package service

import (
	"context"
	"encoding/json"
	"math"
	"testing"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/config"
	"github.com/LuckyKuang/sub2api-plus/internal/domain"
	"github.com/LuckyKuang/sub2api-plus/internal/pkg/timezone"
	"github.com/stretchr/testify/require"
)

func TestCompositeProductRequestPricingAcrossAllRegisteredPlatforms(t *testing.T) {
	for _, platform := range domain.ConcretePlatformIDs() {
		for _, tc := range []struct {
			name                 string
			peak, expected, rate float64
		}{{"standard", 1, 12, 6}, {"peak", 2, 24, 12}} {
			t.Run(platform+"/"+tc.name, func(t *testing.T) {
				logs := &openAIRecordUsageLogRepoStub{inserted: true}
				ledger := &openAIRecordUsageBillingRepoStub{}
				svc := newGatewayRecordUsageServiceWithBillingRepoForTest(logs, ledger, &openAIRecordUsageUserRepoStub{}, &openAIRecordUsageSubRepoStub{})
				group := &Group{ID: 77, Platform: PlatformComposite, RateMultiplier: 1, SubscriptionType: SubscriptionTypeSubscription,
					PeakRateEnabled: true, PeakStart: "09:00", PeakEnd: "12:00", PeakRateMultiplier: tc.peak,
					BillingRatePolicy: domain.BillingRatePolicy{PlatformMultipliers: map[string]float64{platform: 2}, ModelMultipliers: []domain.BillingModelRate{
						{Platform: platform, Model: "model001", Multiplier: 3},
						{Platform: platform, Model: "supplier-target", Multiplier: 99},
					}}, ModelPricing: []ChannelModelPricing{{Platform: platform, Models: []string{"supplier-target"}, BillingMode: BillingModePerRequest, PerRequestPrice: billingFloat(2)}}}
				user := &User{ID: 22}
				key := &APIKey{ID: 11, GroupID: &group.ID, Group: group, User: user, Quota: 100}
				account := &Account{ID: 33, Platform: platform, RateMultiplier: billingFloat(17)}
				ctx := svc.WithBillingPricingSnapshot(WithResolvedTargetPlatform(context.Background(), platform), key,
					time.Date(2026, 10, 20, 10, 30, 0, 0, timezone.Location()), "model001")
				// Routing and account model mapping change the unit-price target;
				// the retail model factor continues to match the public alias.
				input := &RecordUsageInput{Result: &ForwardResult{RequestID: "matrix-" + platform + tc.name, Model: "supplier-target", Usage: ClaudeUsage{InputTokens: 10}},
					APIKey: key, User: user, Account: account, Subscription: &UserSubscription{ID: 44}, OriginalModel: "model001", APIKeyService: &openAIRecordUsageAPIKeyQuotaStub{}}
				require.InDelta(t, tc.expected, svc.QuoteUsageCost(ctx, input), 1e-12)
				deps := svc.inflightEstimateDeps()
				deps.accountMappedModels = func(context.Context, *APIKey, string) []string { return []string{"supplier-target"} }
				reserved, priced := deps.estimate(ctx, key, InflightEstimateRequest{Model: "model001", Kind: InflightEstimatePerRequest})
				require.True(t, priced)
				require.InDelta(t, tc.expected, reserved, 1e-12)
				require.NoError(t, svc.RecordUsage(ctx, input))
				require.NotNil(t, ledger.lastCmd)
				require.InDelta(t, tc.expected, ledger.lastCmd.SubscriptionCost, 1e-12, "$2 × base1 × platform2 × public-model3 × peak")
				require.InDelta(t, tc.expected, ledger.lastCmd.APIKeyQuotaCost, 1e-12)
				require.Equal(t, 2.0, logs.lastLog.TotalCost)
				require.Equal(t, tc.rate, logs.lastLog.RateMultiplier)
				require.Equal(t, "model001", logs.lastLog.BillingRateSnapshot["model"])
				require.Equal(t, platform, logs.lastLog.BillingRateSnapshot["platform"])
				require.Equal(t, 3.0, logs.lastLog.BillingRateSnapshot["model_multiplier"])
				require.Equal(t, tc.peak, logs.lastLog.BillingRateSnapshot["peak_multiplier"])
			})
		}
	}
}

func TestCompositeProductMissingFactorsAndPriceSources(t *testing.T) {
	for _, platform := range domain.ConcretePlatformIDs() {
		for _, tc := range []struct {
			name                    string
			platformRate, modelRate *float64
			custom                  bool
			expected                float64
		}{
			{"both", billingFloat(2), billingFloat(3), true, 12},
			{"model_defaults_one", billingFloat(2), nil, true, 4},
			{"platform_defaults_one", nil, billingFloat(3), true, 6},
			{"both_default_one", nil, nil, true, 2},
			{"catalog_without_custom_price", billingFloat(2), billingFloat(3), false, 12},
		} {
			t.Run(platform+"/"+tc.name, func(t *testing.T) {
				group := &Group{ID: 77, Platform: PlatformComposite, RateMultiplier: 1}
				if tc.platformRate != nil {
					group.BillingRatePolicy.PlatformMultipliers = map[string]float64{platform: *tc.platformRate}
				}
				if tc.modelRate != nil {
					group.BillingRatePolicy.ModelMultipliers = []domain.BillingModelRate{{Platform: platform, Model: "model001", Multiplier: *tc.modelRate}}
				}
				if tc.custom {
					group.ModelPricing = []ChannelModelPricing{{Platform: platform, Models: []string{"supplier-target"}, BillingMode: BillingModeToken, InputPrice: billingFloat(2e-6), OutputPrice: billingFloat(4e-6)}}
				}
				billing := NewBillingService(&config.Config{}, &PricingService{pricingData: map[string]*LiteLLMModelPricing{"supplier-target": {InputCostPerToken: 2e-6, OutputCostPerToken: 4e-6}}})
				key := &APIKey{GroupID: &group.ID, Group: group}
				ctx := withBillingPricingSnapshot(WithResolvedTargetPlatform(context.Background(), platform), key, time.Now(), billing, nil, nil, "model001")
				cost, err := billing.CalculateTokenCostForRequest(TokenCostRequest{Ctx: ctx, Model: "supplier-target", Group: group, Tokens: UsageTokens{InputTokens: 1_000_000}, Resolver: NewModelPricingResolver(nil, billing)})
				require.NoError(t, err)
				require.InDelta(t, tc.expected, cost.ActualCost, 1e-12)
			})
		}
	}
}

func TestCompositeProductSamePublicNameKeepsSupplierPricesAndFactorsSeparate(t *testing.T) {
	group := &Group{ID: 77, Platform: PlatformComposite, RateMultiplier: 1,
		BillingRatePolicy: domain.BillingRatePolicy{PlatformMultipliers: map[string]float64{PlatformOpenAI: 2, PlatformAnthropic: 5}, ModelMultipliers: []domain.BillingModelRate{
			{Platform: PlatformOpenAI, Model: "model001", Multiplier: 3}, {Platform: PlatformAnthropic, Model: "model001", Multiplier: 7},
		}}, ModelPricing: []ChannelModelPricing{
			{Platform: PlatformOpenAI, Models: []string{"model001"}, BillingMode: BillingModePerRequest, PerRequestPrice: billingFloat(2)},
			{Platform: PlatformAnthropic, Models: []string{"model001"}, BillingMode: BillingModePerRequest, PerRequestPrice: billingFloat(4)},
		}}
	billing := NewBillingService(&config.Config{}, nil)
	for _, tc := range []struct {
		platform string
		expected float64
	}{{PlatformOpenAI, 12}, {PlatformAnthropic, 140}} {
		t.Run(tc.platform, func(t *testing.T) {
			ctx := withBillingPricingSnapshot(WithResolvedTargetPlatform(context.Background(), tc.platform), &APIKey{Group: group}, time.Now(), billing, nil, nil, "model001")
			cost, err := billing.CalculateCostUnified(CostInput{Ctx: ctx, Model: "model001", Group: group, GroupID: &group.ID, RequestCount: 1, Resolver: NewModelPricingResolver(nil, billing)})
			require.NoError(t, err)
			require.Equal(t, tc.expected, cost.ActualCost)
		})
	}
}

func TestBillingPeakScopeDurableCompatibilityAndFrozenAllMeters(t *testing.T) {
	for _, tc := range []struct {
		name, scope, calculation string
		request, token           float64
	}{
		{"legacy_missing_scope", "", BillingRateCalculationLegacyOverride, 3, 6},
		{"product_missing_scope", "", BillingRateCalculationProduct, 6, 12},
		{"explicit_token_only", BillingPeakScopeTokenOnly, BillingRateCalculationProduct, 6, 12},
		{"all_meters", BillingPeakScopeAllMeters, BillingRateCalculationProduct, 12, 12},
	} {
		t.Run(tc.name, func(t *testing.T) {
			group := &Group{ID: 77, Platform: PlatformComposite, RateMultiplier: 1,
				BillingRatePolicy: domain.BillingRatePolicy{PlatformMultipliers: map[string]float64{PlatformOpenAI: 2}, ModelMultipliers: []domain.BillingModelRate{{Platform: PlatformOpenAI, Model: "model001", Multiplier: 3}}}}
			stored := durableBillingPricingSnapshot{Version: 1, RateCalculation: tc.calculation, PeakScope: tc.scope, Group: group, TokenPeak: billingFloat(2),
				PricingAt: time.Date(2026, 10, 20, 10, 30, 0, 0, time.UTC), Platform: PlatformOpenAI, PublicModel: "model001",
				Models: map[string]*ModelPricing{"model001": nil}, MissingModels: map[string]bool{"model001": true}, Images: map[string]map[string]float64{}, Videos: map[string]map[string]float64{}, CacheStorage: map[string]float64{}, DeepSeekPeakMultiplier: 1}
			data, err := json.Marshal(stored)
			require.NoError(t, err)
			ctx, err := RestoreBillingPricingSnapshot(context.Background(), data)
			require.NoError(t, err)
			require.Equal(t, tc.request, customerBillingRate(ctx, nil, "request", 99))
			require.Equal(t, tc.token, customerBillingRate(ctx, nil, "token", 99))
			data, err = ExportBillingPricingSnapshot(ctx)
			require.NoError(t, err)
			var persisted durableBillingPricingSnapshot
			require.NoError(t, json.Unmarshal(data, &persisted))
			require.NotEmpty(t, persisted.PeakScope)
			ctx, err = RestoreBillingPricingSnapshot(context.Background(), data)
			require.NoError(t, err)
			require.Equal(t, tc.request, customerBillingRate(ctx, nil, "request", 99))
		})
	}
}

func TestBillingPeakScopeRejectsUnknownDurableScope(t *testing.T) {
	group := billingPolicyTestGroup()
	ctx := withBillingPricingSnapshot(WithResolvedTargetPlatform(context.Background(), PlatformOpenAI), &APIKey{Group: group}, time.Now(), nil, nil, nil, "public-model")
	data, err := ExportBillingPricingSnapshot(ctx)
	require.NoError(t, err)
	var stored map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(data, &stored))
	stored["peak_scope"] = json.RawMessage(`"unknown_scope"`)
	data, err = json.Marshal(stored)
	require.NoError(t, err)
	_, err = RestoreBillingPricingSnapshot(context.Background(), data)
	require.ErrorContains(t, err, "unsupported billing peak scope")
}

func TestCompositeUnknownUnitPriceRejectsSettlementWithoutFinancialWrites(t *testing.T) {
	for _, platform := range domain.ConcretePlatformIDs() {
		for _, gateway := range []string{"generic", "openai"} {
			t.Run(platform+"/"+gateway, func(t *testing.T) {
				logs := &openAIRecordUsageLogRepoStub{inserted: true}
				ledger := &openAIRecordUsageBillingRepoStub{}
				users := &openAIRecordUsageUserRepoStub{}
				subs := &openAIRecordUsageSubRepoStub{}
				quota := &openAIRecordUsageAPIKeyQuotaStub{}
				group := &Group{ID: 77, Platform: PlatformComposite, RateMultiplier: 1,
					BillingRatePolicy: domain.BillingRatePolicy{PlatformMultipliers: map[string]float64{platform: 2}, ModelMultipliers: []domain.BillingModelRate{{Platform: platform, Model: "model001", Multiplier: 3}}},
					ModelPricing:      []ChannelModelPricing{{Platform: platform, Models: []string{"no-price-evidence-model"}, BillingMode: BillingModeToken}}}
				user := &User{ID: 22}
				key := &APIKey{ID: 11, GroupID: &group.ID, Group: group, User: user, Quota: 100}
				account := &Account{ID: 33, Platform: platform}
				ctx := WithResolvedTargetPlatform(context.Background(), platform)
				var err error
				if gateway == "generic" {
					svc := newGatewayRecordUsageServiceWithBillingRepoForTest(logs, ledger, users, subs)
					input := &RecordUsageInput{Result: &ForwardResult{RequestID: "unpriced", Model: "no-price-evidence-model", Usage: ClaudeUsage{InputTokens: 20, OutputTokens: 10}, SearchCount: 1},
						APIKey: key, User: user, Account: account, OriginalModel: "model001", APIKeyService: quota}
					require.True(t, math.IsNaN(svc.QuoteUsageCost(ctx, input)), "a paid search component cannot hide missing token prices")
					err = svc.RecordUsage(ctx, input)
				} else {
					svc := newOpenAIRecordUsageServiceWithBillingRepoForTest(logs, ledger, users, subs, nil)
					err = svc.RecordUsage(ctx, &OpenAIRecordUsageInput{Result: &OpenAIForwardResult{RequestID: "unpriced", Model: "no-price-evidence-model", Usage: OpenAIUsage{InputTokens: 20, OutputTokens: 10}, SearchCount: 1},
						APIKey: key, User: user, Account: account, OriginalModel: "model001", APIKeyService: quota})
				}
				require.ErrorIs(t, err, ErrModelPricingUnavailable)
				require.Zero(t, logs.calls)
				require.Zero(t, ledger.calls)
				require.Zero(t, users.deductCalls)
				require.Zero(t, subs.incrementCalls)
				require.Zero(t, quota.quotaCalls)
				require.Zero(t, quota.rateLimitCalls)
			})
		}
	}
}

func TestUnknownFixedChargeCannotBecomeFreeReservation(t *testing.T) {
	for _, amount := range []float64{math.NaN(), math.Inf(1), math.Inf(-1), -1} {
		res, err := (&BillingCacheService{}).ReserveFixedCharge(context.Background(), &User{ID: 1}, nil, nil, nil, amount)
		require.ErrorIs(t, err, ErrBillingRateUnavailable)
		require.Nil(t, res)
	}
	res, err := (&BillingCacheService{}).ReserveFixedCharge(context.Background(), &User{ID: 1}, nil, nil, nil, 0)
	require.NoError(t, err)
	require.Nil(t, res, "known explicit zero remains free")
}
