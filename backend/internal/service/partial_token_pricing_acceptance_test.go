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
	"github.com/stretchr/testify/require"
)

func TestPartialUnknownTokenCardRequiresOnlyActuallyUsedPrices(t *testing.T) {
	for _, tc := range []struct {
		name, missing string
		card          ChannelModelPricing
		usage         UsageTokens
		expected      float64
	}{
		{"missing_output", "output", ChannelModelPricing{InputPrice: billingFloat(2e-6)}, UsageTokens{InputTokens: 1000, OutputTokens: 1000}, 0},
		{"input_only", "", ChannelModelPricing{InputPrice: billingFloat(2e-6)}, UsageTokens{InputTokens: 1000}, .002},
		{"explicit_zero_output", "", ChannelModelPricing{InputPrice: billingFloat(2e-6), OutputPrice: billingFloat(0)}, UsageTokens{InputTokens: 1000, OutputTokens: 1000}, .002},
		{"output_only", "", ChannelModelPricing{OutputPrice: billingFloat(4e-6)}, UsageTokens{OutputTokens: 1000}, .004},
		{"missing_input", "input", ChannelModelPricing{OutputPrice: billingFloat(4e-6)}, UsageTokens{InputTokens: 1000, OutputTokens: 1000}, 0},
		{"missing_cache_read", "cache_read", ChannelModelPricing{InputPrice: billingFloat(2e-6)}, UsageTokens{CacheReadTokens: 1000}, 0},
		{"zero_cache_read", "", ChannelModelPricing{CacheReadPrice: billingFloat(0)}, UsageTokens{CacheReadTokens: 1000}, 0},
		{"missing_cache_write", "cache_write", ChannelModelPricing{InputPrice: billingFloat(2e-6)}, UsageTokens{CacheCreationTokens: 1000}, 0},
		{"zero_cache_write", "", ChannelModelPricing{CacheWritePrice: billingFloat(0)}, UsageTokens{CacheCreationTokens: 1000}, 0},
		{"one_hour_cache", "", ChannelModelPricing{CacheWrite1hPrice: billingFloat(3e-6)}, UsageTokens{CacheCreationTokens: 1000, CacheCreation1hTokens: 1000}, .003},
		{"zero_one_hour_cache", "", ChannelModelPricing{CacheWrite1hPrice: billingFloat(0)}, UsageTokens{CacheCreationTokens: 1000, CacheCreation1hTokens: 1000}, 0},
		{"one_hour_does_not_price_five_minutes", "cache_write", ChannelModelPricing{CacheWrite1hPrice: billingFloat(3e-6)}, UsageTokens{CacheCreationTokens: 1000, CacheCreation5mTokens: 1000}, 0},
		{"image_input_falls_back_to_known_text", "", ChannelModelPricing{InputPrice: billingFloat(2e-6)}, UsageTokens{InputTokens: 1000, ImageInputTokens: 1000}, .002},
		{"explicit_image_input", "", ChannelModelPricing{ImageInputPrice: billingFloat(3e-6)}, UsageTokens{InputTokens: 1000, ImageInputTokens: 1000}, .003},
		{"zero_image_input_needs_text_fallback", "image_input", ChannelModelPricing{ImageInputPrice: billingFloat(0)}, UsageTokens{InputTokens: 1000, ImageInputTokens: 1000}, 0},
		{"missing_image_output", "image_output", ChannelModelPricing{InputPrice: billingFloat(2e-6)}, UsageTokens{OutputTokens: 1000, ImageOutputTokens: 1000}, 0},
		{"image_output_falls_back_to_known_text", "", ChannelModelPricing{OutputPrice: billingFloat(4e-6)}, UsageTokens{OutputTokens: 1000, ImageOutputTokens: 1000}, .004},
		{"explicit_zero_image_output", "", ChannelModelPricing{ImageOutputPrice: billingFloat(0)}, UsageTokens{OutputTokens: 1000, ImageOutputTokens: 1000}, 0},
		{"interval_does_not_price_output", "output", ChannelModelPricing{Intervals: []PricingInterval{{InputPrice: billingFloat(2e-6)}}}, UsageTokens{InputTokens: 1000, OutputTokens: 1000}, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			billing := NewBillingService(&config.Config{}, nil)
			resolver := NewModelPricingResolver(nil, billing)
			tc.card.BillingMode = BillingModeToken
			resolved := resolver.resolveConfiguredPricing(&tc.card, "unknown-partial-model", PricingSourceChannel)
			cost, err := billing.CalculateCostUnified(CostInput{Ctx: context.Background(), Model: "unknown-partial-model", Tokens: tc.usage, RateMultiplier: 1, Resolver: resolver, Resolved: resolved})
			if tc.missing != "" {
				require.ErrorIs(t, err, ErrModelPricingUnavailable)
				require.ErrorContains(t, err, "missing "+tc.missing+" token price")
				require.Nil(t, cost)
				return
			}
			require.NoError(t, err)
			require.InDelta(t, tc.expected, cost.ActualCost, 1e-12)
		})
	}
}

func TestPartialPublicTokenCardCannotFallBackToMappedGPTAndLoseItsInputPrice(t *testing.T) {
	for _, gateway := range []string{"generic", "openai"} {
		for _, tc := range []struct {
			name         string
			output       *float64
			outputTokens int
			expected     float64
			reject       bool
		}{
			{"unknown_output", nil, 1000, 0, true},
			{"input_only_embeddings", nil, 0, .012, false},
			{"explicit_free_output", billingFloat(0), 1000, .012, false},
			{"explicit_paid_output", billingFloat(4e-6), 1000, .036, false},
		} {
			t.Run(gateway+"/"+tc.name, func(t *testing.T) {
				logs := &openAIRecordUsageLogRepoStub{inserted: true}
				ledger := &openAIRecordUsageBillingRepoStub{}
				users, subs := &openAIRecordUsageUserRepoStub{}, &openAIRecordUsageSubRepoStub{}
				group := &Group{ID: 77, Platform: PlatformComposite, RateMultiplier: 1,
					ModelPricing:      []ChannelModelPricing{{Platform: PlatformOpenAI, Models: []string{"model001"}, BillingMode: BillingModeToken, InputPrice: billingFloat(2e-6), OutputPrice: tc.output}},
					BillingRatePolicy: domain.BillingRatePolicy{PlatformMultipliers: map[string]float64{PlatformOpenAI: 2}, ModelMultipliers: []domain.BillingModelRate{{Platform: PlatformOpenAI, Model: "model001", Multiplier: 3}}}}
				user := &User{ID: 22}
				key := &APIKey{ID: 11, Group: group, GroupID: &group.ID, User: user}
				account := &Account{ID: 33, Platform: PlatformOpenAI}
				billing := NewBillingService(&config.Config{}, nil)
				ctx := withBillingPricingSnapshot(WithResolvedTargetPlatform(context.Background(), PlatformOpenAI), key, time.Now(), billing, nil, nil, "model001")
				// Presence/absence is private durable evidence, not a float-zero flag.
				data, err := ExportBillingPricingSnapshot(ctx, "gpt-5.4")
				require.NoError(t, err)
				var stored durableBillingPricingSnapshot
				require.NoError(t, json.Unmarshal(data, &stored))
				require.Equal(t, tc.output, stored.Group.ModelPricing[0].OutputPrice)
				ctx, err = RestoreBillingPricingSnapshot(context.Background(), data)
				require.NoError(t, err)
				deps := inflightEstimateDeps{billing: billing, resolver: NewModelPricingResolver(nil, billing), accountMappedModels: func(context.Context, *APIKey, string) []string {
					t.Fatal("an authoritative partial public card must not switch wholesale to GPT pricing")
					return nil
				}}
				kind := InflightEstimateToken
				if tc.outputTokens == 0 {
					kind = InflightEstimateInputOnly
				}
				amount, priced := deps.estimate(ctx, key, InflightEstimateRequest{Model: "gpt-5.4", BodyBytes: 4000, MaxTokens: 1000, Kind: kind})
				if tc.reject {
					require.False(t, priced)
					require.Zero(t, amount)
				} else {
					require.True(t, priced)
					require.InDelta(t, tc.expected, amount, 1e-12)
				}
				if gateway == "generic" {
					svc := newGatewayRecordUsageServiceWithBillingRepoForTest(logs, ledger, users, subs)
					input := &RecordUsageInput{Result: &ForwardResult{RequestID: "partial-generic", Model: "gpt-5.4", UpstreamModel: "gpt-5.4", Usage: ClaudeUsage{InputTokens: 1000, OutputTokens: tc.outputTokens}}, APIKey: key, User: user, Account: account, OriginalModel: "model001"}
					quote := svc.QuoteUsageCost(ctx, input)
					if tc.reject {
						require.True(t, math.IsNaN(quote))
					} else {
						require.InDelta(t, tc.expected, quote, 1e-12)
					}
					err = svc.RecordUsage(ctx, input)
				} else {
					svc := newOpenAIRecordUsageServiceWithBillingRepoForTest(logs, ledger, users, subs, nil)
					err = svc.RecordUsage(ctx, &OpenAIRecordUsageInput{Result: &OpenAIForwardResult{RequestID: "partial-openai", Model: "gpt-5.4", BillingModel: "gpt-5.4", UpstreamModel: "gpt-5.4", Usage: OpenAIUsage{InputTokens: 1000, OutputTokens: tc.outputTokens}}, APIKey: key, User: user, Account: account, OriginalModel: "model001"})
				}
				if tc.reject {
					require.ErrorIs(t, err, ErrModelPricingUnavailable)
					require.Zero(t, logs.calls)
					require.Zero(t, ledger.calls)
					return
				}
				require.NoError(t, err)
				require.InDelta(t, tc.expected, ledger.lastCmd.BalanceCost, 1e-12)
			})
		}
	}
}

func TestPartialPublicTokenCatalogKeepsMissingOutputUnknownAndKnownZeroExplicit(t *testing.T) {
	for _, tc := range []struct {
		name   string
		output *float64
		status string
	}{{"unknown", nil, "unknown"}, {"explicit_zero", billingFloat(0), "resolved"}} {
		t.Run(tc.name, func(t *testing.T) {
			group := Group{ID: 77, Platform: PlatformComposite, RateMultiplier: 1, ModelAllowlist: GroupModelAllowlist{Enabled: true, Models: []string{"model001"}},
				ModelPricing: []ChannelModelPricing{{Platform: PlatformOpenAI, Models: []string{"model001"}, BillingMode: BillingModeToken, InputPrice: billingFloat(2e-6), OutputPrice: tc.output}}}
			plaza := newPlazaServiceWithBilling(nil, nil, nil, nil)
			plaza.SetCommercialCatalogSources(&compositeRouteRepoStub{routes: []CompositeModelRoute{{ID: 1, GroupID: 77, PublicModel: "model001", MatchType: CompositeRouteMatchExact, Endpoint: CompositeRouteEndpointEmbeddings,
				TargetPlatform: PlatformOpenAI, UpstreamModel: "text-embedding-3-small", Enabled: true}}}, nil)
			items, err := plaza.ListAvailableCatalog(context.Background(), []Group{group})
			require.NoError(t, err)
			require.Len(t, items[0].Models, 1)
			offer := items[0].Models[0]
			require.Equal(t, tc.status, offer.PriceStatus)
			require.NotNil(t, offer.Pricing)
			require.Equal(t, 2e-6, *offer.Pricing.InputPrice)
			require.Equal(t, tc.output, offer.Pricing.OutputPrice)
			require.Nil(t, offer.Pricing.CacheReadPrice, "unconfigured dimensions are unknown, not zero")
		})
	}
}

func TestPartialKnownModelCardStillInheritsSameNameCatalogOutput(t *testing.T) {
	billing := newTestBillingServiceForResolver()
	group := &Group{ID: 77, Platform: PlatformAnthropic, RateMultiplier: 1, ModelPricing: []ChannelModelPricing{{Platform: PlatformAnthropic, Models: []string{"claude-sonnet-4"}, BillingMode: BillingModeToken, InputPrice: billingFloat(1e-6)}}}
	resolver := NewModelPricingResolver(nil, billing)
	cost, err := billing.CalculateCostUnified(CostInput{Ctx: context.Background(), Model: "claude-sonnet-4", Group: group, GroupID: &group.ID, Tokens: UsageTokens{InputTokens: 1000, OutputTokens: 1000}, RateMultiplier: 1, Resolver: resolver})
	require.NoError(t, err)
	require.InDelta(t, .016, cost.ActualCost, 1e-12, "$1 custom input/M + $15 inherited output/M")
}

func TestPartialTokenChannelIntervalsAndZeroEvidenceSurviveRestore(t *testing.T) {
	group := &Group{ID: 77, Platform: PlatformComposite, RateMultiplier: 1, LongContextPricingEnabled: true}
	card := ChannelModelPricing{Platform: PlatformOpenAI, Models: []string{"model001"}, BillingMode: BillingModeToken,
		InputPrice: billingFloat(2e-6), CacheReadPrice: billingFloat(0), Intervals: []PricingInterval{{MinTokens: 1000, OutputPrice: billingFloat(4e-6)}}}
	cache := populateChannelCache([]Channel{{ID: 1, Status: StatusActive, GroupIDs: []int64{77}, ModelPricing: []ChannelModelPricing{card}}}, map[int64]string{77: PlatformComposite})
	channel := &ChannelService{frozenCache: cache}
	channel.cache.Store(cache)
	billing := NewBillingService(&config.Config{}, nil)
	ctx := withBillingPricingSnapshot(WithResolvedTargetPlatform(context.Background(), PlatformOpenAI), &APIKey{Group: group, GroupID: &group.ID}, time.Now(), billing, channel, nil, "model001")
	data, err := ExportBillingPricingSnapshot(ctx)
	require.NoError(t, err)
	ctx, err = RestoreBillingPricingSnapshot(context.Background(), data)
	require.NoError(t, err)
	for _, tc := range []struct {
		name     string
		usage    UsageTokens
		expected float64
		reject   bool
	}{
		{"missing_flat_output", UsageTokens{InputTokens: 10, OutputTokens: 10}, 0, true},
		{"priced_context_output", UsageTokens{InputTokens: 2000, OutputTokens: 1000}, .008, false},
		{"explicit_free_cache_read", UsageTokens{CacheReadTokens: 1000}, 0, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cost, err := billing.CalculateTokenCostForRequest(TokenCostRequest{Ctx: ctx, Model: "model001", Group: group, Tokens: tc.usage, Resolver: NewModelPricingResolver(nil, billing)})
			if tc.reject {
				require.ErrorIs(t, err, ErrModelPricingUnavailable)
				require.Nil(t, cost)
				return
			}
			require.NoError(t, err)
			require.InDelta(t, tc.expected, cost.ActualCost, 1e-12)
		})
	}
}

func TestPartialKnownPublicCardKeepsInheritedOutputAcrossMappedSettlements(t *testing.T) {
	for _, gateway := range []string{"generic", "openai"} {
		t.Run(gateway, func(t *testing.T) {
			logs := &openAIRecordUsageLogRepoStub{inserted: true}
			ledger := &openAIRecordUsageBillingRepoStub{}
			group := &Group{ID: 77, Platform: PlatformComposite, RateMultiplier: 1,
				ModelPricing:      []ChannelModelPricing{{Platform: PlatformOpenAI, Models: []string{"claude-sonnet-4"}, BillingMode: BillingModeToken, InputPrice: billingFloat(1e-6)}},
				BillingRatePolicy: domain.BillingRatePolicy{PlatformMultipliers: map[string]float64{PlatformOpenAI: 2}, ModelMultipliers: []domain.BillingModelRate{{Platform: PlatformOpenAI, Model: "claude-sonnet-4", Multiplier: 3}}}}
			user := &User{ID: 22}
			key := &APIKey{ID: 11, Group: group, GroupID: &group.ID, User: user}
			account := &Account{ID: 33, Platform: PlatformOpenAI}
			billing := newTestBillingServiceForResolver()
			ctx := withBillingPricingSnapshot(WithResolvedTargetPlatform(context.Background(), PlatformOpenAI), key, time.Now(), billing, nil, nil, "claude-sonnet-4")
			data, err := ExportBillingPricingSnapshot(ctx, "gpt-5.4")
			require.NoError(t, err)
			ctx, err = RestoreBillingPricingSnapshot(context.Background(), data)
			require.NoError(t, err)
			amount, priced := (inflightEstimateDeps{billing: billing, resolver: NewModelPricingResolver(nil, billing)}).estimate(ctx, key, InflightEstimateRequest{Model: "gpt-5.4", BodyBytes: 4000, MaxTokens: 1000})
			require.True(t, priced)
			require.InDelta(t, .096, amount, 1e-12)
			if gateway == "generic" {
				svc := newGatewayRecordUsageServiceWithBillingRepoForTest(logs, ledger, &openAIRecordUsageUserRepoStub{}, &openAIRecordUsageSubRepoStub{})
				input := &RecordUsageInput{Result: &ForwardResult{RequestID: "known-partial", Model: "gpt-5.4", UpstreamModel: "gpt-5.4", Usage: ClaudeUsage{InputTokens: 1000, OutputTokens: 1000}}, APIKey: key, User: user, Account: account, OriginalModel: "claude-sonnet-4"}
				require.InDelta(t, .096, svc.QuoteUsageCost(ctx, input), 1e-12)
				err = svc.RecordUsage(ctx, input)
			} else {
				svc := newOpenAIRecordUsageServiceWithBillingRepoForTest(logs, ledger, &openAIRecordUsageUserRepoStub{}, &openAIRecordUsageSubRepoStub{}, nil)
				err = svc.RecordUsage(ctx, &OpenAIRecordUsageInput{Result: &OpenAIForwardResult{RequestID: "known-partial", Model: "gpt-5.4", BillingModel: "gpt-5.4", UpstreamModel: "gpt-5.4", Usage: OpenAIUsage{InputTokens: 1000, OutputTokens: 1000}}, APIKey: key, User: user, Account: account, OriginalModel: "claude-sonnet-4"})
			}
			require.NoError(t, err)
			require.InDelta(t, .096, ledger.lastCmd.BalanceCost, 1e-12, "($1 custom input/M + $15 same-name catalog output/M) ×1000 ×P2 ×M3")
		})
	}
}

func TestOutputOnlyTokenCatalogRetainsKnownOutputWithoutInventingInput(t *testing.T) {
	for _, tc := range []struct {
		name   string
		output float64
	}{
		{"positive", 4e-6},
		{"explicit_zero", 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			group := Group{ID: 77, Platform: PlatformComposite, RateMultiplier: 1, ModelAllowlist: GroupModelAllowlist{Enabled: true, Models: []string{"output-only-alias"}},
				ModelPricing: []ChannelModelPricing{{Platform: PlatformOpenAI, Models: []string{"output-only-alias"}, BillingMode: BillingModeToken, OutputPrice: billingFloat(tc.output)}}}
			plaza := newPlazaServiceWithBilling(nil, nil, nil, nil)
			plaza.SetCommercialCatalogSources(&compositeRouteRepoStub{routes: []CompositeModelRoute{{ID: 1, GroupID: 77, PublicModel: "output-only-alias", MatchType: CompositeRouteMatchExact, Endpoint: CompositeRouteEndpointResponses,
				TargetPlatform: PlatformOpenAI, UpstreamModel: "gpt-6-astra", Enabled: true}}}, nil)
			items, err := plaza.ListAvailableCatalog(context.Background(), []Group{group})
			require.NoError(t, err)
			require.Len(t, items[0].Models, 1)
			offer := items[0].Models[0]
			require.Equal(t, "unknown", offer.PriceStatus, "the general quote still lacks an input price")
			require.NotNil(t, offer.Pricing)
			require.Nil(t, offer.Pricing.InputPrice, "context probes cannot declare a real input price")
			require.NotNil(t, offer.Pricing.OutputPrice, "a context placeholder cannot hide the configured output price")
			require.InDelta(t, tc.output, *offer.Pricing.OutputPrice, 1e-15)
		})
	}
}

func TestOutputOnlyContextIntervalsKeepTheirIndependentPrices(t *testing.T) {
	group := &Group{ID: 77, Platform: PlatformOpenAI, LongContextPricingEnabled: true}
	card := ChannelModelPricing{Platform: PlatformOpenAI, Models: []string{"output-only-interval"}, BillingMode: BillingModeToken, OutputPrice: billingFloat(2e-6),
		Intervals: []PricingInterval{{MinTokens: 1000, OutputPrice: billingFloat(6e-6)}}}
	cache := populateChannelCache([]Channel{{ID: 1, Status: StatusActive, GroupIDs: []int64{77}, ModelPricing: []ChannelModelPricing{card}}}, map[int64]string{77: PlatformOpenAI})
	channel := &ChannelService{frozenCache: cache}
	channel.cache.Store(cache)
	billing := NewBillingService(&config.Config{}, nil)
	resolver := NewModelPricingResolver(channel, billing)
	schedule, err := billing.ResolveContextPricingSchedule(context.Background(), resolver, ContextPricingScheduleInput{Model: "output-only-interval", Group: group, Platform: PlatformOpenAI})
	require.NoError(t, err)
	require.Len(t, schedule.Tiers, 2)
	for i, expected := range []float64{2e-6, 6e-6} {
		require.Nil(t, schedule.Tiers[i].Input)
		require.NotNil(t, schedule.Tiers[i].Output)
		require.InDelta(t, expected, *schedule.Tiers[i].Output, 1e-15)
	}
	_, err = billing.CalculateTokenCostForRequest(TokenCostRequest{Ctx: context.Background(), Model: "output-only-interval", Group: group, Tokens: UsageTokens{InputTokens: 2000, OutputTokens: 1000}, Resolver: resolver})
	require.ErrorIs(t, err, ErrModelPricingUnavailable, "display probes cannot weaken real input-usage evidence")
	resolved := resolver.Resolve(context.Background(), PricingInput{Model: "output-only-interval", Group: group, GroupID: &group.ID})
	require.Nil(t, resolved.channelPricing.InputPrice, "the shared saved card must remain unchanged")
}

func TestExplicitFreeOneHourCacheInheritsCatalogFiveMinutePrice(t *testing.T) {
	const model = "gpt-6-astra"
	for _, interval := range []bool{false, true} {
		name := "flat"
		card := ChannelModelPricing{Platform: PlatformOpenAI, Models: []string{model}, BillingMode: BillingModeToken, CacheWrite1hPrice: billingFloat(0)}
		if interval {
			name = "interval"
			card.CacheWrite1hPrice = nil
			card.Intervals = []PricingInterval{{MinTokens: 1000, CacheWrite1hPrice: billingFloat(0)}}
		}
		t.Run(name, func(t *testing.T) {
			group := &Group{ID: 77, Platform: PlatformOpenAI, RateMultiplier: 1, LongContextPricingEnabled: true}
			cache := populateChannelCache([]Channel{{ID: 1, Status: StatusActive, GroupIDs: []int64{77}, ModelPricing: []ChannelModelPricing{card}}}, map[int64]string{77: PlatformOpenAI})
			channel := &ChannelService{frozenCache: cache}
			channel.cache.Store(cache)
			billing := NewBillingService(&config.Config{}, nil)
			ctx := withBillingPricingSnapshot(context.Background(), &APIKey{Group: group, GroupID: &group.ID}, time.Now(), billing, channel, nil, model)
			data, err := ExportBillingPricingSnapshot(ctx)
			require.NoError(t, err)
			restored, err := RestoreBillingPricingSnapshot(context.Background(), data)
			require.NoError(t, err)
			for _, snapshot := range []struct {
				name string
				ctx  context.Context
			}{{"live", ctx}, {"restored", restored}} {
				t.Run(snapshot.name, func(t *testing.T) {
					belowExpected := 0.0
					if interval {
						belowExpected = .0125
					}
					for _, usage := range []struct {
						name     string
						tokens   UsageTokens
						expected float64
					}{
						{"five_minutes_inherits", UsageTokens{CacheCreationTokens: 2000, CacheCreation5mTokens: 2000}, .025},
						{"one_hour_is_free", UsageTokens{CacheCreationTokens: 2000, CacheCreation1hTokens: 2000}, 0},
						{"mixed_ttls", UsageTokens{CacheCreationTokens: 2000, CacheCreation5mTokens: 1000, CacheCreation1hTokens: 1000}, .0125},
						{"below_interval_boundary", UsageTokens{CacheCreationTokens: 1000, CacheCreation1hTokens: 1000}, belowExpected},
					} {
						t.Run(usage.name, func(t *testing.T) {
							cost, err := billing.CalculateTokenCostForRequest(TokenCostRequest{Ctx: snapshot.ctx, Model: model, Group: group, Tokens: usage.tokens, RateMultiplier: 1, Resolver: NewModelPricingResolver(channel, billing)})
							require.NoError(t, err)
							require.InDelta(t, usage.expected, cost.ActualCost, 1e-12)
						})
					}
				})
			}
		})
	}
}
