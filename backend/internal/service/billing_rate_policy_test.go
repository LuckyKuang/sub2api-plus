//go:build unit

package service

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/config"
	"github.com/LuckyKuang/sub2api-plus/internal/domain"
	"github.com/LuckyKuang/sub2api-plus/internal/pkg/timezone"
	"github.com/stretchr/testify/require"
)

func billingPolicyTestGroup() *Group {
	return &Group{ID: 77, Platform: PlatformComposite, RateMultiplier: 2, BillingPolicyVersion: 8,
		BillingRatePolicy: domain.BillingRatePolicy{PlatformMultipliers: map[string]float64{PlatformOpenAI: 3},
			ModelMultipliers: []domain.BillingModelRate{{Platform: PlatformOpenAI, Model: "public-model", Multiplier: 4}}}}
}

func TestBillingRateQuoteCustomerPrecedenceAndMediaUnits(t *testing.T) {
	at := time.Date(2026, 10, 20, 10, 30, 0, 0, timezone.Location())
	group := billingPolicyTestGroup()
	group.SubscriptionType, group.PeakRateEnabled = SubscriptionTypeSubscription, true
	group.PeakStart, group.PeakEnd, group.PeakRateMultiplier = "09:00", "12:00", 2
	personal := 0.5
	quote := ResolveBillingRateQuote(group, PlatformOpenAI, "public-model", "token", &personal, at)
	require.Equal(t, 12.0, quote.EffectiveMultiplier, "0.5 personal × 3 platform × 4 model × 2 peak")
	require.Equal(t, 3.0, quote.PlatformMultiplier)
	require.Equal(t, 4.0, quote.ModelMultiplier)
	require.Equal(t, 12.0, quote.PolicyMultiplier)
	require.Equal(t, BillingRateCalculationProduct, quote.RateCalculation)
	require.Equal(t, BillingPeakScopeAllMeters, quote.PeakScope)
	require.Equal(t, "personal", quote.BaseSource)
	require.Equal(t, "model", quote.Source)
	require.Equal(t, int64(8), quote.PolicyVersion)
	require.Equal(t, 3.0, ResolveBillingRateQuote(group, PlatformOpenAI, "other", "token", &personal, at).EffectiveMultiplier)
	require.Equal(t, 12.0, ResolveBillingRateQuote(group, PlatformOpenAI, "public-model", "request", &personal, at).EffectiveMultiplier)
	group.ImageRateIndependent, group.ImageRateMultiplier = true, 0.25
	group.VideoRateIndependent, group.VideoRateMultiplier = true, 0.75
	require.Equal(t, 6.0, ResolveBillingRateQuote(group, PlatformOpenAI, "public-model", "image", &personal, at).EffectiveMultiplier)
	require.Equal(t, 18.0, ResolveBillingRateQuote(group, PlatformOpenAI, "public-model", "second", &personal, at).EffectiveMultiplier)
	for _, platform := range domain.ConcretePlatformIDs() {
		group.BillingRatePolicy.PlatformMultipliers[platform] = 0.125
		require.Equal(t, 0.125, ResolveBillingRateQuote(group, platform, "unmatched", "request", &personal, at).EffectiveMultiplier)
	}
	group.BillingRatePolicy.ModelMultipliers[0].Multiplier = 0
	require.Zero(t, ResolveBillingRateQuote(group, PlatformOpenAI, "public-model", "token", &personal, at).EffectiveMultiplier)
}

func TestBillingPricingSnapshotPinsCatalogGroupPersonalAndPublicIdentity(t *testing.T) {
	group := billingPolicyTestGroup()
	group.ModelPricing = []ChannelModelPricing{{Platform: PlatformOpenAI, Models: []string{"upstream-model"}, BillingMode: BillingModeToken, InputPrice: billingFloat(2e-6), OutputPrice: billingFloat(4e-6)}}
	group.AccountGroups = []AccountGroup{{Account: &Account{Credentials: map[string]any{"access_token": "DO-NOT-EXPORT"}}}}
	prices := &PricingService{pricingData: map[string]*LiteLLMModelPricing{"catalog-model": {InputCostPerToken: 2e-6, OutputCostPerToken: 4e-6}}}
	billing := NewBillingService(&config.Config{}, prices)
	key := &APIKey{ID: 11, User: &User{ID: 22}, GroupID: i64p(group.ID), Group: group}
	at := time.Date(2026, 10, 20, 8, 0, 0, 0, time.UTC)
	personal := 0.5
	ctx := WithResolvedTargetPlatform(context.Background(), PlatformOpenAI)
	ctx = withBillingPricingSnapshot(ctx, key, at, billing, nil, func(context.Context, int64, int64, float64) (float64, bool, error) { return personal, true, nil }, "public-model")
	snapshot := BillingPricingSnapshotFromContext(ctx)
	require.NoError(t, ValidateBillingPricingSnapshot(ctx))
	group.RateMultiplier = 99
	group.BillingRatePolicy.ModelMultipliers[0].Multiplier = 99
	*group.ModelPricing[0].InputPrice = 99
	personal = 99
	prices.pricingData = map[string]*LiteLLMModelPricing{"catalog-model": {InputCostPerToken: 9e-6, OutputCostPerToken: 9e-6}}
	ctx, frozenKey := prepareUsagePricing(ctx, snapshot, key, &Account{Platform: PlatformOpenAI}, "public-model", "upstream-model")
	require.Equal(t, 6.0, customerBillingRate(ctx, frozenKey, "token", 1))
	cost, err := billing.CalculateTokenCostForRequest(TokenCostRequest{Ctx: ctx, Model: "upstream-model", Group: frozenKey.Group, Tokens: UsageTokens{InputTokens: 1_000_000}, Resolver: snapshot.resolver})
	require.NoError(t, err)
	require.Equal(t, 12.0, cost.ActualCost, "$2 original card × personal0.5 × platform3 × model4")
	catalog, err := snapshot.billing.GetModelPricing("catalog-model")
	require.NoError(t, err)
	require.Equal(t, 2e-6, catalog.InputPricePerToken)
	data, err := ExportBillingPricingSnapshot(ctx, "upstream-model", "catalog-model")
	require.NoError(t, err)
	require.NotContains(t, string(data), "DO-NOT-EXPORT")
	restored, err := RestoreBillingPricingSnapshot(context.Background(), data)
	require.NoError(t, err)
	restored, restoredKey := prepareUsagePricing(restored, nil, key, &Account{Platform: PlatformOpenAI}, "other", "upstream-model")
	restoredSnapshot := BillingPricingSnapshotFromContext(restored)
	require.Equal(t, at, restoredSnapshot.PricingAt())
	require.Equal(t, 6.0, customerBillingRate(restored, restoredKey, "token", 1))
	cost, err = restoredSnapshot.billing.CalculateTokenCostForRequest(TokenCostRequest{Ctx: restored, Model: "upstream-model", Group: restoredKey.Group, Tokens: UsageTokens{InputTokens: 1_000_000}, Resolver: restoredSnapshot.resolver})
	require.NoError(t, err)
	require.Equal(t, 12.0, cost.ActualCost)
	_, err = restoredSnapshot.billing.GetModelPricing("not-captured")
	require.ErrorIs(t, err, ErrModelPricingUnavailable)
	newCtx := withBillingPricingSnapshot(WithResolvedTargetPlatform(context.Background(), PlatformOpenAI), key, at, billing, nil, nil, "public-model")
	require.Equal(t, 29403.0, customerBillingRate(newCtx, key, "token", 1))
}

func TestBillingPricingSnapshotChannelReloadAndConcreteSupplierIsolation(t *testing.T) {
	group := &Group{ID: 77, Platform: PlatformComposite, RateMultiplier: 1,
		ModelPricing: []ChannelModelPricing{
			{Platform: PlatformComposite, Models: []string{"shared"}, BillingMode: BillingModeToken, InputPrice: billingFloat(1e-6)},
			{Platform: PlatformOpenAI, Models: []string{"shared"}, BillingMode: BillingModeToken, InputPrice: billingFloat(2e-6)},
			{Platform: PlatformAnthropic, Models: []string{"shared"}, BillingMode: BillingModeToken, InputPrice: billingFloat(5e-6)},
		}}
	billing := NewBillingService(&config.Config{}, nil)
	for _, tc := range []struct {
		platform string
		price    float64
	}{{PlatformOpenAI, 2}, {PlatformAnthropic, 5}, {PlatformGrok, 1}} {
		ctx := WithResolvedTargetPlatform(context.Background(), tc.platform)
		resolver := NewModelPricingResolver(nil, billing)
		resolved := resolver.Resolve(ctx, PricingInput{Group: group, Model: "shared"})
		require.Equal(t, tc.price/1e6, resolved.BasePricing.InputPricePerToken)
	}
	channels := newChannelServiceWithPricings(group.ID, []ChannelModelPricing{{Platform: PlatformOpenAI, Models: []string{"channel-model"}, BillingMode: BillingModeToken, InputPrice: billingFloat(7e-6)}})
	key := &APIKey{Group: group, GroupID: i64p(group.ID)}
	ctx := withBillingPricingSnapshot(WithResolvedTargetPlatform(context.Background(), PlatformOpenAI), key, time.Now(), billing, channels, nil, "channel-model")
	changed := populateChannelCache([]Channel{{ID: 1, GroupIDs: []int64{group.ID}, ModelPricing: []ChannelModelPricing{{Platform: PlatformOpenAI, Models: []string{"channel-model"}, BillingMode: BillingModeToken, InputPrice: billingFloat(99e-6)}}}}, map[int64]string{group.ID: PlatformComposite})
	channels.cache.Store(changed)
	snapshot := BillingPricingSnapshotFromContext(ctx)
	resolved := snapshot.resolver.Resolve(ctx, PricingInput{Group: group, GroupID: &group.ID, Model: "channel-model"})
	require.Equal(t, 7e-6, resolved.BasePricing.InputPricePerToken)
	data, err := ExportBillingPricingSnapshot(ctx, "channel-model")
	require.NoError(t, err)
	restored, err := RestoreBillingPricingSnapshot(context.Background(), data)
	require.NoError(t, err)
	restored = WithResolvedTargetPlatform(restored, PlatformOpenAI)
	r := BillingPricingSnapshotFromContext(restored).resolver.Resolve(restored, PricingInput{Group: group, GroupID: &group.ID, Model: "channel-model"})
	require.Equal(t, 7e-6, r.BasePricing.InputPricePerToken)
}

func TestBillingPricingSnapshotFailureNeverFallsBack(t *testing.T) {
	group := billingPolicyTestGroup()
	key := &APIKey{Group: group, User: &User{ID: 1}}
	ctx := withBillingPricingSnapshot(context.Background(), key, time.Now(), NewBillingService(&config.Config{}, nil), nil, func(context.Context, int64, int64, float64) (float64, bool, error) {
		return 0, false, errors.New("storage unavailable")
	}, "public-model")
	require.Error(t, ValidateBillingPricingSnapshot(ctx))
	_, err := ExportBillingPricingSnapshot(ctx)
	require.Error(t, err)
	cache := &BillingCacheService{}
	require.ErrorIs(t, cache.CheckBillingEligibility(ctx, nil, nil, nil, nil, ""), ErrBillingRateUnavailable)
	for _, data := range []json.RawMessage{json.RawMessage(`{`), json.RawMessage(`{}`), json.RawMessage(`{"version":2}`), json.RawMessage(`null`)} {
		_, err = RestoreBillingPricingSnapshot(context.Background(), data)
		require.Error(t, err)
	}
	_, err = RestoreBillingPricingSnapshot(context.Background(), nil)
	require.NoError(t, err)
	for _, rate := range []float64{math.Inf(1), math.NaN(), 1e9} {
		invalid := billingPolicyTestGroup()
		invalid.RateMultiplier = rate
		bad := withBillingPricingSnapshot(WithResolvedTargetPlatform(context.Background(), PlatformOpenAI), &APIKey{Group: invalid}, time.Now(), nil, nil, nil, "public-model")
		require.Error(t, ValidateBillingPricingSnapshot(bad))
	}
}

func TestBillingPolicyRecordUsageChargesSharedSubscriptionWithFrozenPublicRule(t *testing.T) {
	for _, gateway := range []string{"generic", "openai"} {
		t.Run(gateway, func(t *testing.T) {
			logs := &openAIRecordUsageLogRepoStub{inserted: true}
			ledger := &openAIRecordUsageBillingRepoStub{}
			user := &User{ID: 22}
			group := billingPolicyTestGroup()
			group.SubscriptionType = SubscriptionTypeSubscription
			group.ModelPricing = []ChannelModelPricing{{Platform: PlatformOpenAI, Models: []string{"gpt-5.1"}, BillingMode: BillingModeToken, InputPrice: billingFloat(2e-6), OutputPrice: billingFloat(4e-6)}}
			key := &APIKey{ID: 11, Group: group, GroupID: &group.ID, User: user, Quota: 100}
			account := &Account{ID: 33, Platform: PlatformOpenAI, RateMultiplier: billingFloat(7)}
			sub := &UserSubscription{ID: 44, UserID: user.ID, GroupID: group.ID}
			ctx := WithResolvedTargetPlatform(context.Background(), PlatformOpenAI)
			personal := func(context.Context, int64, int64, float64) (float64, bool, error) { return 0.5, true, nil }
			at := time.Date(2026, 10, 20, 8, 0, 0, 0, time.UTC)
			if gateway == "generic" {
				svc := newGatewayRecordUsageServiceWithBillingRepoForTest(logs, ledger, &openAIRecordUsageUserRepoStub{}, &openAIRecordUsageSubRepoStub{})
				ctx = withBillingPricingSnapshot(ctx, key, at, svc.billingService, nil, personal, "public-model")
				group.BillingRatePolicy.ModelMultipliers[0].Multiplier = 90
				require.NoError(t, svc.RecordUsage(ctx, &RecordUsageInput{Result: &ForwardResult{RequestID: "policy-generic", Model: "gpt-5.1", Usage: ClaudeUsage{InputTokens: 1000, OutputTokens: 500}}, APIKey: key, User: user, Account: account, Subscription: sub, OriginalModel: "public-model", APIKeyService: &openAIRecordUsageAPIKeyQuotaStub{}}))
			} else {
				svc := newOpenAIRecordUsageServiceWithBillingRepoForTest(logs, ledger, &openAIRecordUsageUserRepoStub{}, &openAIRecordUsageSubRepoStub{}, nil)
				ctx = withBillingPricingSnapshot(ctx, key, at, svc.billingService, nil, personal, "public-model")
				group.BillingRatePolicy.ModelMultipliers[0].Multiplier = 90
				require.NoError(t, svc.RecordUsage(ctx, &OpenAIRecordUsageInput{Result: &OpenAIForwardResult{RequestID: "policy-openai", Model: "gpt-5.1", Usage: OpenAIUsage{InputTokens: 1000, OutputTokens: 500}}, APIKey: key, User: user, Account: account, Subscription: sub, OriginalModel: "public-model", APIKeyService: &openAIRecordUsageAPIKeyQuotaStub{}}))
			}
			require.NotNil(t, logs.lastLog)
			require.InDelta(t, 0.004, logs.lastLog.TotalCost, 1e-12)
			require.InDelta(t, 0.024, logs.lastLog.ActualCost, 1e-12, "($2 input/M + $4 output/M) × frozen personal0.5 × platform3 × model4")
			require.Equal(t, 6.0, logs.lastLog.RateMultiplier)
			require.Equal(t, int64(8), logs.lastLog.BillingPolicyVersion)
			require.Equal(t, "public-model", logs.lastLog.BillingRateSnapshot["model"])
			require.NotNil(t, ledger.lastCmd)
			require.Equal(t, sub.ID, *ledger.lastCmd.SubscriptionID)
			require.InDelta(t, 0.024, ledger.lastCmd.SubscriptionCost, 1e-12)
			require.InDelta(t, 0.024, ledger.lastCmd.APIKeyQuotaCost, 1e-12)
			require.Zero(t, ledger.lastCmd.BalanceCost)
			encoded, err := json.Marshal(logs.lastLog.BillingRateSnapshot)
			require.NoError(t, err)
			for _, forbidden := range []string{"gpt-5.1", "account", "credentials", "InputPrice", "access_token"} {
				require.False(t, strings.Contains(string(encoded), forbidden))
			}
		})
	}
}

func TestBillingPolicyInflightPaidMetersWhenTokenAndImageAreFree(t *testing.T) {
	group := &Group{ID: 77, Platform: PlatformComposite, RateMultiplier: 1, BillingRatePolicy: domain.BillingRatePolicy{ModelMultipliers: []domain.BillingModelRate{
		{Platform: PlatformGrok, Model: "media", BillingUnit: "token", Multiplier: 0},
		{Platform: PlatformGrok, Model: "media", BillingUnit: "image", Multiplier: 0},
		{Platform: PlatformGrok, Model: "media", BillingUnit: "minute", Multiplier: 3},
		{Platform: PlatformGrok, Model: "media", BillingUnit: "second", Multiplier: 4},
		{Platform: PlatformGrok, Model: "media", BillingUnit: "request", Multiplier: 5},
	}}}
	key := &APIKey{Group: group, GroupID: &group.ID, User: &User{ID: 1}}
	billing := NewBillingService(&config.Config{}, nil)
	ctx := withBillingPricingSnapshot(WithResolvedTargetPlatform(context.Background(), PlatformGrok), key, time.Now(), billing, nil, nil, "media")
	deps := inflightEstimateDeps{billing: billing, resolver: NewModelPricingResolver(nil, billing)}
	amount, ok := deps.estimate(ctx, key, InflightEstimateRequest{Model: "media", Kind: InflightEstimateAudio, AudioMode: "realtime", AudioUnits: 2})
	require.True(t, ok)
	require.InDelta(t, 0.30, amount, 1e-12, "2 minutes × $0.05 × rate3")
	group.ModelPricing = []ChannelModelPricing{{Platform: PlatformGrok, Models: []string{"media"}, BillingMode: BillingModeVideo, PerRequestPrice: billingFloat(0.25)}}
	ctx = withBillingPricingSnapshot(WithResolvedTargetPlatform(context.Background(), PlatformGrok), key, time.Now(), billing, nil, nil, "media")
	amount, ok = deps.estimate(ctx, key, InflightEstimateRequest{Model: "media", Kind: InflightEstimateVideo, Units: 2, VideoDurationSeconds: 6})
	require.True(t, ok)
	require.InDelta(t, 12.0, amount, 1e-12, "$0.25/second × 2 videos × 6 seconds × rate4")
	group.ModelPricing[0].BillingMode = BillingModePerRequest
	ctx = withBillingPricingSnapshot(WithResolvedTargetPlatform(context.Background(), PlatformGrok), key, time.Now(), billing, nil, nil, "media")
	amount, ok = deps.estimate(ctx, key, InflightEstimateRequest{Model: "media", Kind: InflightEstimateImage, Units: 2})
	require.True(t, ok)
	require.InDelta(t, 2.5, amount, 1e-12, "per_request card uses request5, not image0")
}

func billingFloat(value float64) *float64 { return &value }

func TestBillingPricingSnapshotChannelErrorCacheCannotBecomeMissingChannelPrice(t *testing.T) {
	key := &APIKey{Group: billingPolicyTestGroup()}
	channels := &ChannelService{repo: &mockChannelRepository{listAllFn: func(context.Context) ([]Channel, error) { return nil, errors.New("channel store unavailable") }}}
	for attempt := 0; attempt < 2; attempt++ {
		failed := withBillingPricingSnapshot(context.Background(), key, time.Now(), NewBillingService(&config.Config{}, nil), channels, nil, "public-model")
		require.Error(t, ValidateBillingPricingSnapshot(failed), "first failure and throttled empty cache both reject")
	}
}

func TestBillingPolicyOpenAIMediaUsageUsesActualMeterInLedgerAndAudit(t *testing.T) {
	for _, tc := range []struct {
		kind, unit     string
		expected, rate float64
	}{
		{"image", "image", 5.4, 54},
		{"video", "second", 54, 108},
		{"audio", "minute", 4.5, 45},
	} {
		t.Run(tc.kind, func(t *testing.T) {
			logs := &openAIRecordUsageLogRepoStub{inserted: true}
			ledger := &openAIRecordUsageBillingRepoStub{}
			svc := newOpenAIRecordUsageServiceWithBillingRepoForTest(logs, ledger, &openAIRecordUsageUserRepoStub{}, &openAIRecordUsageSubRepoStub{}, nil)
			group := &Group{ID: 77, Platform: PlatformComposite, RateMultiplier: 1, SubscriptionType: SubscriptionTypeSubscription,
				PeakRateEnabled: true, PeakStart: "00:00", PeakEnd: "23:59", PeakRateMultiplier: 9,
				ImageRateIndependent: true, ImageRateMultiplier: 2, ImagePrice1K: billingFloat(0.1),
				VideoRateIndependent: true, VideoRateMultiplier: 3, VideoPrice480P: billingFloat(0.1),
				BillingRatePolicy: domain.BillingRatePolicy{ModelMultipliers: []domain.BillingModelRate{
					{Platform: PlatformGrok, Model: "media", BillingUnit: "token", Multiplier: 0},
					{Platform: PlatformGrok, Model: "media", BillingUnit: "image", Multiplier: 3},
					{Platform: PlatformGrok, Model: "media", BillingUnit: "second", Multiplier: 4},
					{Platform: PlatformGrok, Model: "media", BillingUnit: "minute", Multiplier: 5},
				}}}
			user := &User{ID: 22}
			key := &APIKey{ID: 11, GroupID: &group.ID, Group: group, User: user}
			result := &OpenAIForwardResult{RequestID: "policy-media-" + tc.kind, Model: "media"}
			switch tc.kind {
			case "image":
				result.ImageCount = 1
				result.ImageSize = "1K"
			case "video":
				result.VideoCount = 1
				result.VideoDurationSeconds = 5
				result.VideoResolution = "480p"
			case "audio":
				result.AudioUsage = &AudioUsage{Mode: "realtime", DurationOrUnits: 2}
			}
			ctx := WithResolvedTargetPlatform(context.Background(), PlatformGrok)
			ctx = svc.WithBillingPricingSnapshot(ctx, key, time.Date(2026, 10, 20, 10, 30, 0, 0, timezone.Location()), "media")
			err := svc.RecordUsage(ctx, &OpenAIRecordUsageInput{Result: result, APIKey: key, User: user, Account: &Account{ID: 33, Platform: PlatformGrok}, Subscription: &UserSubscription{ID: 44}, OriginalModel: "media"})
			require.NoError(t, err)
			require.InDelta(t, tc.expected, logs.lastLog.ActualCost, 1e-12)
			require.Equal(t, tc.rate, logs.lastLog.RateMultiplier)
			require.Equal(t, tc.unit, logs.lastLog.BillingRateSnapshot["billing_unit"])
			require.Equal(t, 9.0, logs.lastLog.BillingRateSnapshot["peak_multiplier"])
			require.Equal(t, BillingPeakScopeAllMeters, logs.lastLog.BillingRateSnapshot["peak_scope"])
			require.InDelta(t, tc.expected, ledger.lastCmd.SubscriptionCost, 1e-12)
		})
	}
}

func TestBillingPolicyProfitGateUsesFrozenCustomerRate(t *testing.T) {
	for _, platform := range []string{PlatformOpenAI, PlatformAnthropic} {
		t.Run(platform, func(t *testing.T) {
			group := gatewayProfitTestGroup(77, platform)
			group.ProfitMinMargin, group.ProfitSafetyBuffer = 0.2, 0.1
			group.BillingRatePolicy = domain.BillingRatePolicy{PlatformMultipliers: map[string]float64{platform: 4}, ModelMultipliers: []domain.BillingModelRate{{Platform: platform, Model: "public-model", Multiplier: 2}}}
			ctx := gatewayProfitTestContext(group)
			ctx = WithResolvedTargetPlatform(ctx, platform)
			ctx = withBillingPricingSnapshot(ctx, &APIKey{Group: group}, time.Now(), nil, nil, nil, "public-model")
			if platform == PlatformOpenAI {
				svc := &OpenAIGatewayService{}
				ctx, _ = svc.WithOpenAIRequestPricingContext(ctx, &group.ID)
			} else {
				svc := &GatewayService{}
				ctx = svc.withGatewayProfitControlGate(ctx, &group.ID)
			}
			gate, _ := ctx.Value(openAIProfitControlGateCtxKey{}).(*openAIProfitControlGate)
			require.NotNil(t, gate)
			require.InDelta(t, 2.8, gate.threshold, 1e-12, "base0.5 × platform4 × model2 × margin0.7")
			allowed := gatewayProfitTestAccount(1, platform, 2.8, group.ID)
			denied := gatewayProfitTestAccount(2, platform, 2.8001, group.ID)
			veto, _ := openAIProfitControlVetoReason(ctx, &allowed)
			require.False(t, veto)
			veto, _ = openAIProfitControlVetoReason(ctx, &denied)
			require.True(t, veto)
		})
	}
}

func TestBillingPolicyGeminiCacheStorageAndSearchUseSeparateFrozenMeters(t *testing.T) {
	svc, logs := newGeminiCacheBillingGatewayForTest(t)
	ledger := &openAIRecordUsageBillingRepoStub{}
	svc.usageBillingRepo = ledger
	group := &Group{ID: 77, Platform: PlatformComposite, RateMultiplier: 2, Hydrated: true, SubscriptionType: SubscriptionTypeSubscription,
		BillingRatePolicy: domain.BillingRatePolicy{ModelMultipliers: []domain.BillingModelRate{
			{Platform: PlatformGemini, Model: "public-cache", BillingUnit: "token", Multiplier: 3},
			{Platform: PlatformGemini, Model: "public-cache", BillingUnit: "request", Multiplier: 4},
			{Platform: PlatformGemini, Model: "public-cache", BillingUnit: "hour", Multiplier: 5},
		}}}
	user := &User{ID: 22}
	key := &APIKey{ID: 11, GroupID: &group.ID, Group: group, User: user}
	ctx := svc.WithBillingPricingSnapshot(WithResolvedTargetPlatform(context.Background(), PlatformGemini), key, time.Now(), "public-cache")
	input := &RecordUsageInput{Result: &ForwardResult{RequestID: "policy-cache", Model: "gemini-3.8-flash", Usage: ClaudeUsage{InputTokens: 1000}, CacheStorageTokenHours: 1000, SearchCount: 1},
		APIKey: key, User: user, Account: &Account{ID: 33, Platform: PlatformGemini}, Subscription: &UserSubscription{ID: 44}, OriginalModel: "public-cache"}
	// 1000 × $0.75/M × 6 + 1 × $5/1000 × 8 + 1000 token-hours × $0.5/M × 10.
	require.InDelta(t, 0.0495, svc.QuoteUsageCost(ctx, input), 1e-12)
	require.NoError(t, svc.RecordUsage(ctx, input))
	require.InDelta(t, 0.0495, logs.lastLog.ActualCost, 1e-12)
	require.InDelta(t, 0.0495, ledger.lastCmd.SubscriptionCost, 1e-12)
	require.Equal(t, 6.0, logs.lastLog.RateMultiplier)
	components, ok := logs.lastLog.BillingRateSnapshot["components"].([]map[string]any)
	require.True(t, ok)
	require.Len(t, components, 2)
	require.Equal(t, "request", components[0]["billing_unit"])
	require.Equal(t, 8.0, components[0]["effective_multiplier"])
	require.Equal(t, "hour", components[1]["billing_unit"])
	require.Equal(t, 10.0, components[1]["effective_multiplier"])
}

func TestBillingPricingSnapshotDirectSettlementRejectsCaptureFailureBeforeWrites(t *testing.T) {
	for _, gateway := range []string{"generic", "openai"} {
		t.Run(gateway, func(t *testing.T) {
			logs := &openAIRecordUsageLogRepoStub{}
			ledger := &openAIRecordUsageBillingRepoStub{}
			key := &APIKey{Group: &Group{ID: 77, RateMultiplier: math.NaN()}, GroupID: i64p(77)}
			var err error
			if gateway == "generic" {
				svc := newGatewayRecordUsageServiceWithBillingRepoForTest(logs, ledger, &openAIRecordUsageUserRepoStub{}, &openAIRecordUsageSubRepoStub{})
				err = svc.RecordUsage(context.Background(), &RecordUsageInput{Result: &ForwardResult{Model: "gpt-5.1"}, APIKey: key, User: &User{ID: 22}, Account: &Account{ID: 33}})
			} else {
				svc := newOpenAIRecordUsageServiceWithBillingRepoForTest(logs, ledger, &openAIRecordUsageUserRepoStub{}, &openAIRecordUsageSubRepoStub{}, nil)
				err = svc.RecordUsage(context.Background(), &OpenAIRecordUsageInput{Result: &OpenAIForwardResult{Model: "gpt-5.1"}, APIKey: key, User: &User{ID: 22}, Account: &Account{ID: 33}})
			}
			require.Error(t, err)
			require.Zero(t, ledger.calls)
			require.Zero(t, logs.calls)
		})
	}
}

func TestBillingPricingSnapshotDurableMediaMissingModelNeverBecomesFree(t *testing.T) {
	group := &Group{ID: 77, Platform: PlatformGrok, RateMultiplier: 1, ImagePrice1K: billingFloat(0)}
	user := &User{ID: 22}
	key := &APIKey{ID: 11, Group: group, GroupID: &group.ID, User: user}
	billing := NewBillingService(&config.Config{}, nil)
	ctx := withBillingPricingSnapshot(context.Background(), key, time.Now(), billing, nil, nil, "known-image")
	data, err := ExportBillingPricingSnapshot(ctx, "known-image")
	require.NoError(t, err)
	ctx, err = RestoreBillingPricingSnapshot(context.Background(), data)
	require.NoError(t, err)
	for _, tc := range []struct {
		model  string
		reject bool
	}{{"unknown-image", true}, {"known-image", false}} {
		t.Run(tc.model, func(t *testing.T) {
			logs := &openAIRecordUsageLogRepoStub{inserted: true}
			ledger := &openAIRecordUsageBillingRepoStub{}
			svc := newOpenAIRecordUsageServiceWithBillingRepoForTest(logs, ledger, &openAIRecordUsageUserRepoStub{}, &openAIRecordUsageSubRepoStub{}, nil)
			err := svc.RecordUsage(ctx, &OpenAIRecordUsageInput{Result: &OpenAIForwardResult{RequestID: tc.model, Model: tc.model, ImageCount: 1, ImageSize: "1K"}, APIKey: key, User: user, Account: &Account{ID: 33, Platform: PlatformGrok}})
			if tc.reject {
				require.Error(t, err)
				require.Zero(t, logs.calls)
				require.Zero(t, ledger.calls)
			} else {
				require.NoError(t, err)
				require.Zero(t, logs.lastLog.ActualCost)
				require.Equal(t, "image", logs.lastLog.BillingRateSnapshot["billing_unit"])
			}
		})
	}
}

func TestBillingPricingSnapshotPolicyOnlyRoundtripPreservesMissingPriceEvidence(t *testing.T) {
	key := &APIKey{Group: billingPolicyTestGroup()}
	ctx := withBillingPricingSnapshot(WithResolvedTargetPlatform(context.Background(), PlatformOpenAI), key, time.Now(), nil, nil, nil, "public-task")
	data, err := ExportBillingPricingSnapshot(ctx, "upstream-task")
	require.NoError(t, err)
	restored, err := RestoreBillingPricingSnapshot(context.Background(), data)
	require.NoError(t, err)
	data, err = ExportBillingPricingSnapshot(restored, "upstream-task")
	require.NoError(t, err)
	var stored durableBillingPricingSnapshot
	require.NoError(t, json.Unmarshal(data, &stored))
	require.True(t, stored.MissingModels["public-task"])
	require.True(t, stored.MissingModels["upstream-task"])
	require.NotContains(t, stored.Images, "upstream-task")
	require.NotContains(t, stored.Videos, "upstream-task")
	require.Error(t, validateDurableBillingModel(restored, "upstream-task"), "missing price evidence cannot authorize a charge")
	_, err = ExportBillingPricingSnapshot(restored, "newly-invented-model")
	require.Error(t, err)
}

func TestBillingPricingSnapshotFreezesTokenPeakAcrossTimezoneRestart(t *testing.T) {
	// Keep this test sequential: timezone is process configuration, shared by
	// all services. Restore it before any parallel service tests resume.
	previous := timezone.Name()
	require.NoError(t, timezone.Init("UTC"))
	t.Cleanup(func() { require.NoError(t, timezone.Init(previous)) })
	group := &Group{ID: 77, Platform: PlatformOpenAI, RateMultiplier: 1, SubscriptionType: SubscriptionTypeSubscription, PeakRateEnabled: true, PeakStart: "00:00", PeakEnd: "01:00", PeakRateMultiplier: 3}
	key := &APIKey{Group: group, GroupID: &group.ID}
	at := time.Date(2026, 10, 20, 0, 30, 0, 0, time.UTC)
	billing := NewBillingService(&config.Config{}, nil)
	ctx := withBillingPricingSnapshot(context.Background(), key, at, billing, nil, nil, "gpt-5.1")
	require.Equal(t, 3.0, customerBillingRate(ctx, key, "token", 1))
	require.Equal(t, 3.0, customerBillingRate(ctx, key, "request", 1))
	data, err := ExportBillingPricingSnapshot(ctx, "gpt-5.1")
	require.NoError(t, err)
	require.NoError(t, timezone.Init("Asia/Shanghai"))
	require.Equal(t, 3.0, customerBillingRate(ctx, key, "token", 1), "admitted request retains the captured UTC peak")
	restored, err := RestoreBillingPricingSnapshot(context.Background(), data)
	require.NoError(t, err)
	require.Equal(t, 3.0, customerBillingRate(restored, key, "token", 1), "restart cannot reinterpret the historical timestamp in its new timezone")
	require.Equal(t, 3.0, customerBillingRate(restored, key, "request", 1), "all-meter peaks are captured numerically across timezone changes")
	next := withBillingPricingSnapshot(context.Background(), key, at, billing, nil, nil, "gpt-5.1")
	require.Equal(t, 1.0, customerBillingRate(next, key, "token", 1), "a new request uses current Shanghai business time")
	var fields map[string]any
	require.NoError(t, json.Unmarshal(data, &fields))
	for _, peak := range []any{nil, -1.0} {
		fields["token_peak"] = peak
		invalid, err := json.Marshal(fields)
		require.NoError(t, err)
		_, err = RestoreBillingPricingSnapshot(context.Background(), invalid)
		require.Error(t, err)
	}
}

func TestBillingPricingSnapshotLegacyMarkerAlsoAppliesToLaterCapture(t *testing.T) {
	ctx, err := RestoreBillingPricingSnapshot(context.Background(), nil)
	require.NoError(t, err)
	group := billingPolicyTestGroup()
	key := &APIKey{Group: group, GroupID: &group.ID}
	ctx = withBillingPricingSnapshot(WithResolvedTargetPlatform(ctx, PlatformOpenAI), key, time.Now(), NewBillingService(&config.Config{}, nil), nil, nil, "public-model")
	quote := BillingPricingSnapshotFromContext(ctx).quote(PlatformOpenAI, "public-model", "token")
	require.Equal(t, 2.0, quote.EffectiveMultiplier)
	require.Equal(t, 1.0, quote.PolicyMultiplier)
	require.Zero(t, quote.PolicyVersion)
	require.Equal(t, int64(8), group.BillingPolicyVersion)
	require.Equal(t, 4.0, group.BillingRatePolicy.ModelMultipliers[0].Multiplier)
}

func TestBillingPricingSnapshotCalculationVersionPreservesAcceptedTaskCharges(t *testing.T) {
	// These values describe accepted task contracts, independently of the current
	// resolver. In particular, an old model override could override platform zero.
	for _, tc := range []struct {
		name, calculation      string
		platformRate, expected float64
	}{
		{"old_missing", "", 3, 2},
		{"old_explicit", BillingRateCalculationLegacyOverride, 3, 2},
		{"old_platform_zero", "", 0, 2},
		{"product", BillingRateCalculationProduct, 3, 6},
		{"product_platform_zero", BillingRateCalculationProduct, 0, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			personal, peak := .5, 1.0
			stored := durableBillingPricingSnapshot{Version: 1, RateCalculation: tc.calculation,
				Group: &Group{ID: 77, Platform: PlatformOpenAI, RateMultiplier: 2, BillingPolicyVersion: 8,
					BillingRatePolicy: domain.BillingRatePolicy{PlatformMultipliers: map[string]float64{PlatformOpenAI: tc.platformRate}, ModelMultipliers: []domain.BillingModelRate{{Platform: PlatformOpenAI, Model: "public-model", Multiplier: 4}}}},
				PersonalRate: &personal, TokenPeak: &peak, PricingAt: time.Date(2026, 10, 20, 0, 0, 0, 0, time.UTC),
				Platform: PlatformOpenAI, PublicModel: "public-model", Models: map[string]*ModelPricing{"public-model": nil}, MissingModels: map[string]bool{"public-model": true},
				Images: map[string]map[string]float64{}, Videos: map[string]map[string]float64{}, CacheStorage: map[string]float64{}, DeepSeekPeakMultiplier: 1}
			data, err := json.Marshal(stored)
			require.NoError(t, err)
			if tc.calculation == "" {
				var fields map[string]json.RawMessage
				require.NoError(t, json.Unmarshal(data, &fields))
				delete(fields, "rate_calculation")
				data, err = json.Marshal(fields)
				require.NoError(t, err)
			}
			ctx, err := RestoreBillingPricingSnapshot(context.Background(), data)
			require.NoError(t, err)
			quote := BillingPricingSnapshotFromContext(ctx).quote(PlatformOpenAI, "public-model", "token")
			require.Equal(t, tc.expected, quote.EffectiveMultiplier)
			require.Equal(t, tc.platformRate, quote.PlatformMultiplier)
			require.Equal(t, 4.0, quote.ModelMultiplier)
			// Re-persistence must retain the algorithm after a second restart.
			again, err := ExportBillingPricingSnapshot(ctx)
			require.NoError(t, err)
			restarted, err := RestoreBillingPricingSnapshot(context.Background(), again)
			require.NoError(t, err)
			require.Equal(t, quote.RateCalculation, BillingPricingSnapshotFromContext(restarted).calculation())
			require.Equal(t, tc.expected, customerBillingRate(restarted, nil, "token", 99))
		})
	}
}

func TestBillingPricingSnapshotRejectsUnknownCalculation(t *testing.T) {
	group := billingPolicyTestGroup()
	key := &APIKey{Group: group, GroupID: &group.ID}
	ctx := withBillingPricingSnapshot(WithResolvedTargetPlatform(context.Background(), PlatformOpenAI), key, time.Now(), nil, nil, nil, "public-model")
	data, err := ExportBillingPricingSnapshot(ctx)
	require.NoError(t, err)
	var stored map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(data, &stored))
	stored["rate_calculation"] = json.RawMessage(`"unknown_v3"`)
	data, err = json.Marshal(stored)
	require.NoError(t, err)
	_, err = RestoreBillingPricingSnapshot(context.Background(), data)
	require.ErrorContains(t, err, "unsupported billing rate calculation")
}
