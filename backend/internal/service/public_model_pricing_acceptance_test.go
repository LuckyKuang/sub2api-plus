//go:build unit

package service

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/config"
	"github.com/LuckyKuang/sub2api-plus/internal/domain"
	"github.com/LuckyKuang/sub2api-plus/internal/pkg/timezone"
	"github.com/stretchr/testify/require"
)

func TestPublicModelCardSurvivesAccountMappingForBothGatewaySettlements(t *testing.T) {
	for _, groupPlatform := range []string{PlatformOpenAI, PlatformComposite} {
		for _, gateway := range []string{"generic", "openai"} {
			t.Run(groupPlatform+"/"+gateway, func(t *testing.T) {
				logs := &openAIRecordUsageLogRepoStub{inserted: true}
				ledger := &openAIRecordUsageBillingRepoStub{}
				group := &Group{ID: 77, Platform: groupPlatform, RateMultiplier: 1, ModelPricing: []ChannelModelPricing{{
					Platform: PlatformOpenAI, Models: []string{"model001"}, BillingMode: BillingModePerRequest, PerRequestPrice: billingFloat(2),
				}}, BillingRatePolicy: domain.BillingRatePolicy{PlatformMultipliers: map[string]float64{PlatformOpenAI: 2}, ModelMultipliers: []domain.BillingModelRate{{Platform: PlatformOpenAI, Model: "model001", Multiplier: 3}}}}
				user := &User{ID: 22}
				key := &APIKey{ID: 11, Group: group, GroupID: &group.ID, User: user}
				account := &Account{ID: 33, Platform: PlatformOpenAI}
				ctx := WithResolvedTargetPlatform(context.Background(), PlatformOpenAI)
				if gateway == "generic" {
					svc := newGatewayRecordUsageServiceWithBillingRepoForTest(logs, ledger, &openAIRecordUsageUserRepoStub{}, &openAIRecordUsageSubRepoStub{})
					ctx = svc.WithBillingPricingSnapshot(ctx, key, time.Now(), "model001")
					require.NoError(t, svc.RecordUsage(ctx, &RecordUsageInput{Result: &ForwardResult{RequestID: "public-generic", Model: "vendor-model", UpstreamModel: "gpt-5.4", Usage: ClaudeUsage{InputTokens: 1000, OutputTokens: 1000}},
						APIKey: key, User: user, Account: account, OriginalModel: "model001"}))
				} else {
					svc := newOpenAIRecordUsageServiceWithBillingRepoForTest(logs, ledger, &openAIRecordUsageUserRepoStub{}, &openAIRecordUsageSubRepoStub{}, nil)
					ctx = svc.WithBillingPricingSnapshot(ctx, key, time.Now(), "model001")
					require.NoError(t, svc.RecordUsage(ctx, &OpenAIRecordUsageInput{Result: &OpenAIForwardResult{RequestID: "public-openai", Model: "vendor-model", BillingModel: "gpt-5.4", UpstreamModel: "gpt-5.4", Usage: OpenAIUsage{InputTokens: 1000, OutputTokens: 1000}},
						APIKey: key, User: user, Account: account, OriginalModel: "model001"}))
				}
				require.NotNil(t, ledger.lastCmd)
				require.InDelta(t, 12.0, ledger.lastCmd.BalanceCost, 1e-12, "public $2 card × platform2 × public model3; mapped GPT catalog cannot replace it")
				require.Equal(t, 2.0, logs.lastLog.TotalCost)
				require.Equal(t, "model001", logs.lastLog.BillingRateSnapshot["model"])
			})
		}
	}
}

func TestPublicModelPricingPreservesExplicitChannelBillingSources(t *testing.T) {
	for _, source := range []string{BillingModelSourceRequested, BillingModelSourceChannelMapped, BillingModelSourceUpstream, BillingModelSourceResponse} {
		t.Run(source, func(t *testing.T) {
			group := &Group{ID: 77, Platform: PlatformComposite, RateMultiplier: 1, ModelPricing: []ChannelModelPricing{{Platform: PlatformOpenAI, Models: []string{"model001"}, BillingMode: BillingModePerRequest, PerRequestPrice: billingFloat(2)}}}
			cache := populateChannelCache([]Channel{{ID: 1, GroupIDs: []int64{77}, Status: StatusActive, BillingModelSource: source,
				ModelMapping: map[string]map[string]string{PlatformOpenAI: {"model001": "gpt-5.4"}},
				ModelPricing: []ChannelModelPricing{{Platform: PlatformOpenAI, Models: []string{"gpt-5.4"}, BillingMode: BillingModePerRequest, PerRequestPrice: billingFloat(5)}},
			}}, map[int64]string{77: PlatformComposite})
			channel := &ChannelService{frozenCache: cache}
			channel.cache.Store(cache)
			logs := &openAIRecordUsageLogRepoStub{inserted: true}
			ledger := &openAIRecordUsageBillingRepoStub{}
			svc := newOpenAIRecordUsageServiceWithBillingRepoForTest(logs, ledger, &openAIRecordUsageUserRepoStub{}, &openAIRecordUsageSubRepoStub{}, nil)
			svc.channelService, svc.resolver = channel, NewModelPricingResolver(channel, svc.billingService)
			user := &User{ID: 22}
			key := &APIKey{ID: 11, Group: group, GroupID: &group.ID, User: user}
			ctx := svc.WithBillingPricingSnapshot(WithResolvedTargetPlatform(context.Background(), PlatformOpenAI), key, time.Now(), "model001")
			require.NoError(t, svc.RecordUsage(ctx, &OpenAIRecordUsageInput{Result: &OpenAIForwardResult{RequestID: "source-" + source, Model: "gpt-5.4", BillingModel: "gpt-5.4", UpstreamModel: "gpt-5.4", Usage: OpenAIUsage{InputTokens: 1000, OutputTokens: 1000}},
				APIKey: key, User: user, Account: &Account{ID: 33, Platform: PlatformOpenAI}, ChannelUsageFields: ChannelUsageFields{OriginalModel: "model001", ChannelMappedModel: "gpt-5.4", BillingModelSource: source}}))
			want := 5.0
			if source == BillingModelSourceRequested {
				want = 2
			}
			require.Equal(t, want, ledger.lastCmd.BalanceCost)
		})
	}
}

func TestPublicModelCatalogAndReservationUsePublicCardAfterCompositeRoute(t *testing.T) {
	group := Group{ID: 77, Platform: PlatformComposite, RateMultiplier: 1,
		ModelAllowlist:    GroupModelAllowlist{Enabled: true, Models: []string{"model001"}},
		ModelPricing:      []ChannelModelPricing{{Platform: PlatformOpenAI, Models: []string{"model001"}, BillingMode: BillingModePerRequest, PerRequestPrice: billingFloat(2)}},
		BillingRatePolicy: domain.BillingRatePolicy{PlatformMultipliers: map[string]float64{PlatformOpenAI: 2}, ModelMultipliers: []domain.BillingModelRate{{Platform: PlatformOpenAI, Model: "model001", Multiplier: 3}}}}
	plaza := newPlazaServiceWithBilling(nil, nil, nil, nil)
	plaza.SetCommercialCatalogSources(&compositeRouteRepoStub{routes: []CompositeModelRoute{{ID: 1, GroupID: 77, PublicModel: "model001", MatchType: CompositeRouteMatchExact,
		Endpoint: CompositeRouteEndpointChatCompletions, TargetPlatform: PlatformOpenAI, UpstreamModel: "gpt-5.4", Enabled: true}}}, nil)
	items, err := plaza.ListAvailableCatalog(context.Background(), []Group{group})
	require.NoError(t, err)
	require.Len(t, items[0].Models, 1)
	offer := items[0].Models[0]
	require.Equal(t, "model001", offer.Name)
	require.Equal(t, "resolved", offer.PriceStatus)
	require.Equal(t, BillingModePerRequest, offer.BillingMode)
	require.Equal(t, 2.0, *offer.Pricing.PerRequestPrice)
	require.Equal(t, 6.0, offer.ResolveBillingRateQuote(&group, nil, items[0].AsOf).EffectiveMultiplier)
	key := &APIKey{ID: 11, Group: &group, GroupID: &group.ID, User: &User{ID: 22}}
	ctx := withBillingPricingSnapshot(WithResolvedTargetPlatform(context.Background(), PlatformOpenAI), key, time.Now(), plaza.billingService, nil, nil, "model001")
	deps := inflightEstimateDeps{billing: plaza.billingService, resolver: NewModelPricingResolver(nil, plaza.billingService), accountMappedModels: func(context.Context, *APIKey, string) []string {
		t.Fatal("a fully priced public card must not reserve a mapped SKU")
		return nil
	}}
	amount, priced := deps.estimate(ctx, key, InflightEstimateRequest{Model: "vendor-model", BodyBytes: 4000, MaxTokens: 1_000_000})
	require.True(t, priced)
	require.Equal(t, 12.0, amount)
}

func TestPublicTokenPriceCombinesPlatformModelPeakAndChannelTimeOnce(t *testing.T) {
	at := time.Date(2026, 10, 20, 10, 30, 0, 0, timezone.Location())
	group := &Group{ID: 77, Platform: PlatformComposite, RateMultiplier: 1, SubscriptionType: SubscriptionTypeSubscription,
		PeakRateEnabled: true, PeakStart: "10:00", PeakEnd: "11:00", PeakRateMultiplier: 1.5,
		BillingRatePolicy: domain.BillingRatePolicy{PlatformMultipliers: map[string]float64{PlatformOpenAI: 2}, ModelMultipliers: []domain.BillingModelRate{{Platform: PlatformOpenAI, Model: "model001", Multiplier: 3}}}}
	card := ChannelModelPricing{Platform: PlatformOpenAI, Models: []string{"model001"}, BillingMode: BillingModeToken,
		InputPrice: billingFloat(2e-6), OutputPrice: billingFloat(0), TimePricing: &ChannelTimePricing{Timezone: timezone.Name(),
			Periods: []ChannelTimePricingPeriod{{StartTime: "10:00", EndTime: "11:00", Multiplier: 2}}}}
	ch := Channel{ID: 1, GroupIDs: []int64{77}, Status: StatusActive, ModelPricing: []ChannelModelPricing{card}}
	cache := populateChannelCache([]Channel{ch}, map[int64]string{77: PlatformComposite})
	channel := &ChannelService{frozenCache: cache}
	channel.cache.Store(cache)
	logs := &openAIRecordUsageLogRepoStub{inserted: true}
	ledger := &openAIRecordUsageBillingRepoStub{}
	svc := newGatewayRecordUsageServiceWithBillingRepoForTest(logs, ledger, &openAIRecordUsageUserRepoStub{}, &openAIRecordUsageSubRepoStub{})
	svc.channelService, svc.resolver = channel, NewModelPricingResolver(channel, svc.billingService)
	user := &User{ID: 22}
	key := &APIKey{ID: 11, Group: group, GroupID: &group.ID, User: user}
	ctx := svc.WithBillingPricingSnapshot(WithResolvedTargetPlatform(context.Background(), PlatformOpenAI), key, at, "model001")
	input := &RecordUsageInput{Result: &ForwardResult{RequestID: "token-all-factors", Model: "vendor-model", UpstreamModel: "gpt-5.4", Usage: ClaudeUsage{InputTokens: 1_000_000}},
		APIKey: key, User: user, Account: &Account{ID: 33, Platform: PlatformOpenAI}, OriginalModel: "model001"}
	require.Equal(t, 36.0, svc.QuoteUsageCost(ctx, input), "$2/M ×1M ×P2 ×M3 ×peak1.5 ×channel-time2")
	cfg := &config.Config{}
	cfg.Billing.InflightReservation.MaxInputTokens = 1_000_000
	amount, priced := (inflightEstimateDeps{cfg: cfg, billing: svc.billingService, resolver: svc.resolver}).estimate(ctx, key,
		InflightEstimateRequest{Model: "vendor-model", BodyBytes: 4_000_000, MaxTokens: 1})
	require.True(t, priced)
	require.Equal(t, 36.0, amount)
	require.NoError(t, svc.RecordUsage(ctx, input))
	require.Equal(t, 36.0, ledger.lastCmd.BalanceCost)
	require.Equal(t, 4.0, logs.lastLog.TotalCost, "unit-price period affects base cost, while group peak is a selling factor")
	require.Equal(t, 9.0, logs.lastLog.RateMultiplier)
	plaza := newPlazaServiceWithBilling([]Channel{ch}, nil, map[int64]string{77: PlatformComposite}, nil)
	offer := plaza.catalogOffer(WithResolvedTargetPlatform(context.Background(), PlatformOpenAI), group, &ch,
		SupportedModel{Name: "gpt-5.4", Platform: PlatformOpenAI}, "model001", NewModelPricingResolver(channel, plaza.billingService), NewChannelPricingReferenceService(plaza.pricingService, plaza.billingService), at)
	require.Equal(t, "resolved", offer.PriceStatus)
	require.Equal(t, 2e-6, *offer.Pricing.InputPrice)
	require.Equal(t, 2.0, offer.PriceTimeMultiplier)
	require.Equal(t, 9.0, offer.ResolveBillingRateQuote(group, nil, at).EffectiveMultiplier)
}

func TestDurablePublicPriceSelectionPreservesAcceptedVersionOneTask(t *testing.T) {
	group := &Group{ID: 77, Platform: PlatformComposite, RateMultiplier: 1, ModelPricing: []ChannelModelPricing{
		{Platform: PlatformOpenAI, Models: []string{"model001"}, BillingMode: BillingModePerRequest, PerRequestPrice: billingFloat(2)},
		{Platform: PlatformOpenAI, Models: []string{"upstream-target"}, BillingMode: BillingModePerRequest, PerRequestPrice: billingFloat(5)},
	}}
	key := &APIKey{ID: 11, Group: group, GroupID: &group.ID, User: &User{ID: 22}}
	billing := NewBillingService(&config.Config{}, nil)
	ctx := withBillingPricingSnapshot(WithResolvedTargetPlatform(context.Background(), PlatformOpenAI), key, time.Now(), billing, nil, nil, "model001")
	data, err := ExportBillingPricingSnapshot(ctx, "upstream-target")
	require.NoError(t, err)
	var stored durableBillingPricingSnapshot
	require.NoError(t, json.Unmarshal(data, &stored))
	require.Equal(t, 2, stored.Version, "new admissions persist public-card selection")
	for _, tc := range []struct {
		version  int
		expected float64
	}{{1, 5}, {2, 2}} {
		t.Run(map[int]string{1: "accepted_legacy_task", 2: "new_task"}[tc.version], func(t *testing.T) {
			stored.Version = tc.version
			encoded, err := json.Marshal(stored)
			require.NoError(t, err)
			restored, err := RestoreBillingPricingSnapshot(context.Background(), encoded)
			require.NoError(t, err)
			reexported, err := ExportBillingPricingSnapshot(restored, "upstream-target")
			require.NoError(t, err)
			var again durableBillingPricingSnapshot
			require.NoError(t, json.Unmarshal(reexported, &again))
			require.Equal(t, tc.version, again.Version, "restarting a legacy task must not adopt new selection")
			logs := &openAIRecordUsageLogRepoStub{inserted: true}
			ledger := &openAIRecordUsageBillingRepoStub{}
			svc := newOpenAIRecordUsageServiceWithBillingRepoForTest(logs, ledger, &openAIRecordUsageUserRepoStub{}, &openAIRecordUsageSubRepoStub{}, nil)
			require.NoError(t, svc.RecordUsage(restored, &OpenAIRecordUsageInput{Result: &OpenAIForwardResult{RequestID: "delayed", Model: "upstream-target", BillingModel: "upstream-target", UpstreamModel: "upstream-target", Usage: OpenAIUsage{InputTokens: 100}},
				APIKey: key, User: key.User, Account: &Account{ID: 33, Platform: PlatformOpenAI}, OriginalModel: "model001"}))
			require.Equal(t, tc.expected, ledger.lastCmd.BalanceCost)
		})
	}
	stored.Version = 3
	unknown, err := json.Marshal(stored)
	require.NoError(t, err)
	_, err = RestoreBillingPricingSnapshot(context.Background(), unknown)
	require.ErrorContains(t, err, "unsupported or incomplete")
}

func TestPublicRouteThenChannelMappingKeepsExplicitPriceSource(t *testing.T) {
	for _, source := range []string{BillingModelSourceRequested, BillingModelSourceChannelMapped} {
		t.Run(source, func(t *testing.T) {
			group := &Group{ID: 77, Platform: PlatformComposite, RateMultiplier: 1, ModelPricing: []ChannelModelPricing{{Platform: PlatformOpenAI, Models: []string{"model001"}, BillingMode: BillingModePerRequest, PerRequestPrice: billingFloat(2)}},
				BillingRatePolicy: domain.BillingRatePolicy{PlatformMultipliers: map[string]float64{PlatformOpenAI: 2}, ModelMultipliers: []domain.BillingModelRate{{Platform: PlatformOpenAI, Model: "model001", Multiplier: 3}, {Platform: PlatformOpenAI, Model: "bill-model", Multiplier: 99}}}}
			ch := Channel{ID: 1, Status: StatusActive, GroupIDs: []int64{77}, BillingModelSource: source,
				ModelMapping: map[string]map[string]string{PlatformOpenAI: {"vendor-model": "bill-model", "other-route": "wrong-bill"}},
				ModelPricing: []ChannelModelPricing{{Platform: PlatformOpenAI, Models: []string{"bill-model"}, BillingMode: BillingModePerRequest, PerRequestPrice: billingFloat(5)}, {Platform: PlatformOpenAI, Models: []string{"wrong-bill"}, BillingMode: BillingModePerRequest, PerRequestPrice: billingFloat(9)}}}
			cache := populateChannelCache([]Channel{ch}, map[int64]string{77: PlatformComposite})
			channel := &ChannelService{frozenCache: cache}
			channel.cache.Store(cache)
			logs := &openAIRecordUsageLogRepoStub{inserted: true}
			ledger := &openAIRecordUsageBillingRepoStub{}
			svc := newGatewayRecordUsageServiceWithBillingRepoForTest(logs, ledger, &openAIRecordUsageUserRepoStub{}, &openAIRecordUsageSubRepoStub{})
			svc.channelService, svc.resolver = channel, NewModelPricingResolver(channel, svc.billingService)
			user := &User{ID: 22}
			key := &APIKey{ID: 11, Group: group, GroupID: &group.ID, User: user}
			ctx := WithCompositeRouteDecision(context.Background(), CompositeRouteDecision{Matched: true, TargetPlatform: PlatformOpenAI, PublicModel: "model001", UpstreamModel: "vendor-model"})
			ctx = svc.WithBillingPricingSnapshot(ctx, key, time.Now(), "model001")
			detached := CopyBillingPricingSnapshot(ctx, context.Background())
			_, retainedRouteContext := ResolvedUpstreamModelFromContext(detached)
			require.False(t, retainedRouteContext, "only accepted snapshot is copied")
			require.Equal(t, "vendor-model", BillingPricingSnapshotFromContext(detached).routingModel)
			data, err := ExportBillingPricingSnapshot(ctx, "vendor-model", "bill-model", "gpt-5.4")
			require.NoError(t, err)
			ctx, err = RestoreBillingPricingSnapshot(context.Background(), data)
			require.NoError(t, err)
			require.Equal(t, "vendor-model", BillingPricingSnapshotFromContext(ctx).routingModel)
			ctx = WithCompositeRouteDecision(ctx, CompositeRouteDecision{Matched: true, TargetPlatform: PlatformOpenAI, PublicModel: "model001", UpstreamModel: "other-route"})
			reexported, err := ExportBillingPricingSnapshot(ctx)
			require.NoError(t, err, "current routing context cannot add an uncaptured new model")
			var persisted durableBillingPricingSnapshot
			require.NoError(t, json.Unmarshal(reexported, &persisted))
			require.Equal(t, "vendor-model", persisted.RoutingModel)
			ctx, err = RestoreBillingPricingSnapshot(context.Background(), reexported)
			require.NoError(t, err)
			ctx = WithCompositeRouteDecision(ctx, CompositeRouteDecision{Matched: true, TargetPlatform: PlatformOpenAI, PublicModel: "model001", UpstreamModel: "other-route"})
			want := 30.0
			if source == BillingModelSourceRequested {
				want = 12
			}
			input := &RecordUsageInput{Result: &ForwardResult{RequestID: "three-stage-" + source, Model: "bill-model", UpstreamModel: "gpt-5.4", Usage: ClaudeUsage{InputTokens: 1000, OutputTokens: 1000}},
				APIKey: key, User: user, Account: &Account{ID: 33, Platform: PlatformOpenAI}, ChannelUsageFields: ChannelUsageFields{OriginalModel: "model001", ChannelMappedModel: "bill-model", BillingModelSource: source}}
			require.Equal(t, want, svc.QuoteUsageCost(detached, input), "detached usage retains the accepted channel input")
			require.Equal(t, want, svc.QuoteUsageCost(ctx, input))
			require.NoError(t, svc.RecordUsage(ctx, input))
			require.Equal(t, want, ledger.lastCmd.BalanceCost)
			amount, priced := (inflightEstimateDeps{billing: svc.billingService, resolver: svc.resolver}).estimate(ctx, key, InflightEstimateRequest{Model: "vendor-model", BodyBytes: 4000, MaxTokens: 1000})
			require.True(t, priced)
			require.Equal(t, want, amount)
			plaza := newPlazaServiceWithBilling([]Channel{ch}, nil, map[int64]string{77: PlatformComposite}, nil)
			offer := plaza.catalogOffer(WithResolvedTargetPlatform(context.Background(), PlatformOpenAI), group, &ch,
				SupportedModel{Name: "vendor-model", Platform: PlatformOpenAI}, "model001", NewModelPricingResolver(channel, plaza.billingService), NewChannelPricingReferenceService(plaza.pricingService, plaza.billingService), time.Now())
			require.Equal(t, "resolved", offer.PriceStatus)
			require.Equal(t, want, *offer.Pricing.PerRequestPrice*offer.ResolveBillingRateQuote(group, nil, time.Now()).EffectiveMultiplier)
		})
	}
}

func TestPricingSnapshotCapturesRouteAddedAfterInitialSnapshot(t *testing.T) {
	group := &Group{ID: 77, Platform: PlatformComposite, RateMultiplier: 1}
	key := &APIKey{Group: group, GroupID: &group.ID}
	billing := NewBillingService(&config.Config{}, nil)
	initial := withBillingPricingSnapshot(WithResolvedTargetPlatform(context.Background(), PlatformOpenAI), key, time.Now(), billing, nil, nil, "model001")
	before := BillingPricingSnapshotFromContext(initial)
	routed := WithCompositeRouteDecision(initial, CompositeRouteDecision{Matched: true, TargetPlatform: PlatformOpenAI, PublicModel: "model001", UpstreamModel: "vendor-model"})
	captured := withBillingPricingSnapshot(routed, key, time.Now(), billing, nil, nil, "model001")
	require.Equal(t, "vendor-model", BillingPricingSnapshotFromContext(captured).routingModel)
	require.NotSame(t, before, BillingPricingSnapshotFromContext(captured))
	require.Empty(t, before.routingModel, "a later route cannot mutate a shared snapshot")
	detached := CopyBillingPricingSnapshot(routed, context.Background())
	require.Equal(t, "vendor-model", BillingPricingSnapshotFromContext(detached).routingModel)
	require.Empty(t, before.routingModel)
}

func TestLegacyTaskWithoutSnapshotKeepsOriginalPriceOrderAndTokenPeakScope(t *testing.T) {
	for _, gateway := range []string{"generic", "openai"} {
		t.Run(gateway, func(t *testing.T) {
			group := &Group{ID: 77, Platform: PlatformComposite, RateMultiplier: 1, SubscriptionType: SubscriptionTypeSubscription,
				PeakRateEnabled: true, PeakStart: "09:00", PeakEnd: "12:00", PeakRateMultiplier: 2,
				BillingRatePolicy: domain.BillingRatePolicy{PlatformMultipliers: map[string]float64{PlatformOpenAI: 2}, ModelMultipliers: []domain.BillingModelRate{{Platform: PlatformOpenAI, Model: "model001", Multiplier: 3}}},
				ModelPricing: []ChannelModelPricing{
					{Platform: PlatformOpenAI, Models: []string{"model001"}, BillingMode: BillingModePerRequest, PerRequestPrice: billingFloat(2)},
					{Platform: PlatformOpenAI, Models: []string{"upstream-target"}, BillingMode: BillingModePerRequest, PerRequestPrice: billingFloat(5)},
				}}
			user := &User{ID: 22}
			key := &APIKey{ID: 11, Group: group, GroupID: &group.ID, User: user}
			ctx, err := RestoreBillingPricingSnapshot(WithResolvedTargetPlatform(context.Background(), PlatformOpenAI), nil)
			require.NoError(t, err)
			logs := &openAIRecordUsageLogRepoStub{inserted: true}
			ledger := &openAIRecordUsageBillingRepoStub{}
			at := time.Date(2026, 10, 20, 10, 30, 0, 0, timezone.Location())
			if gateway == "generic" {
				svc := newGatewayRecordUsageServiceWithBillingRepoForTest(logs, ledger, &openAIRecordUsageUserRepoStub{}, &openAIRecordUsageSubRepoStub{})
				ctx = svc.WithBillingPricingSnapshot(ctx, key, at, "model001")
				err = svc.RecordUsage(ctx, &RecordUsageInput{Result: &ForwardResult{RequestID: "legacy-no-format", Model: "upstream-target", Usage: ClaudeUsage{InputTokens: 100}}, APIKey: key, User: user, Account: &Account{ID: 33, Platform: PlatformOpenAI}, OriginalModel: "model001"})
			} else {
				svc := newOpenAIRecordUsageServiceWithBillingRepoForTest(logs, ledger, &openAIRecordUsageUserRepoStub{}, &openAIRecordUsageSubRepoStub{}, nil)
				ctx = svc.WithBillingPricingSnapshot(ctx, key, at, "model001")
				err = svc.RecordUsage(ctx, &OpenAIRecordUsageInput{Result: &OpenAIForwardResult{RequestID: "legacy-no-format", Model: "upstream-target", BillingModel: "upstream-target", Usage: OpenAIUsage{InputTokens: 100}}, APIKey: key, User: user, Account: &Account{ID: 33, Platform: PlatformOpenAI}, OriginalModel: "model001"})
			}
			require.NoError(t, err)
			require.True(t, BillingPricingSnapshotFromContext(ctx).legacyPriceSelection)
			require.Equal(t, BillingPeakScopeTokenOnly, BillingPricingSnapshotFromContext(ctx).peakMeterScope())
			require.Equal(t, 5.0, ledger.lastCmd.BalanceCost, "historical tasks keep upstream card and do not acquire new P/M or request peak factors")
		})
	}
}
