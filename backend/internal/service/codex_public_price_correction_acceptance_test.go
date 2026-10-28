//go:build unit

package service

import (
	"context"
	"testing"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/domain"
	"github.com/stretchr/testify/require"
)

// A provider's terminal model observation is diagnostic evidence, not a new
// retail offer. A configured public $2 card and platform2/model3 factors charge
// $12 even when Codex reports a different known model. Explicit free stays free.
func TestCodexServerModelPreservesExplicitPublicPrice(t *testing.T) {
	for _, source := range []string{PricingSourceGroup, PricingSourceChannel} {
		for _, mode := range []BillingMode{BillingModeToken, BillingModePerRequest} {
			for _, free := range []bool{false, true} {
				name := source + "/" + string(mode) + "/paid"
				price, expected := 2.0, 12.0
				if free {
					name, price, expected = source+"/"+string(mode)+"/explicit_zero", 0, 0
				}
				t.Run(name, func(t *testing.T) {
					card := ChannelModelPricing{Platform: PlatformOpenAI, Models: []string{"model001"}, BillingMode: mode}
					if mode == BillingModeToken {
						card.InputPrice, card.OutputPrice = billingFloat(0), &price
					} else {
						card.PerRequestPrice = &price
					}
					svc, key, logs, ledger := codexPublicPriceFixture(source, card, "", false)
					ctx := svc.WithBillingPricingSnapshot(WithResolvedTargetPlatform(context.Background(), PlatformOpenAI), key, time.Now(), "model001")
					result := codexPublicPriceResult()
					require.NoError(t, svc.RecordUsage(ctx, &OpenAIRecordUsageInput{PricingSnapshot: BillingPricingSnapshotFromContext(ctx), Result: result,
						APIKey: key, User: key.User, Account: &Account{ID: 33, Platform: PlatformOpenAI, Type: AccountTypeOAuth}, OriginalModel: "model001"}))
					require.NotNil(t, ledger.lastCmd)
					require.InDelta(t, expected, ledger.lastCmd.BalanceCost, 1e-12, "terminal model must preserve the accepted public price")
					require.NotNil(t, logs.lastLog)
					require.Equal(t, price, logs.lastLog.TotalCost)
					require.Equal(t, "model001", logs.lastLog.BillingRateSnapshot["model"])
					require.NotNil(t, logs.lastLog.UpstreamResponseModel)
					require.Equal(t, "gpt-5.3-codex", *logs.lastLog.UpstreamResponseModel, "preserving retail price must not erase the provider observation")
				})
			}
		}
	}
}

func TestCodexServerModelFallbackAndExplicitSourcesRetainCorrection(t *testing.T) {
	for _, tc := range []struct {
		name     string
		source   string
		card     bool
		empty    bool
		legacy   bool
		wantCost float64
	}{
		{name: "no public card", wantCost: 30},
		{name: "empty public card", card: true, empty: true, wantCost: 30},
		{name: "requested public card", source: BillingModelSourceRequested, card: true, wantCost: 12},
		{name: "explicit upstream source", source: BillingModelSourceUpstream, card: true, wantCost: 30},
		{name: "explicit response source", source: BillingModelSourceResponse, card: true, wantCost: 30},
		{name: "explicit channel mapping", source: BillingModelSourceChannelMapped, card: true, wantCost: 30},
		{name: "accepted legacy selection", card: true, legacy: true, wantCost: 30},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var card ChannelModelPricing
			if tc.card {
				card = ChannelModelPricing{Platform: PlatformOpenAI, Models: []string{"model001"}, BillingMode: BillingModePerRequest}
				if !tc.empty {
					card.PerRequestPrice = billingFloat(2)
				}
			}
			svc, key, logs, ledger := codexPublicPriceFixture(PricingSourceGroup, card, tc.source, true)
			ctx := svc.WithBillingPricingSnapshot(WithResolvedTargetPlatform(context.Background(), PlatformOpenAI), key, time.Now(), "model001")
			if tc.legacy {
				frozen := *BillingPricingSnapshotFromContext(ctx)
				frozen.legacyPriceSelection = true
				ctx = pricingSnapshotContext(ctx, &frozen)
			}
			require.NoError(t, svc.RecordUsage(ctx, &OpenAIRecordUsageInput{PricingSnapshot: BillingPricingSnapshotFromContext(ctx), Result: codexPublicPriceResult(),
				APIKey: key, User: key.User, Account: &Account{ID: 33, Platform: PlatformOpenAI, Type: AccountTypeOAuth},
				ChannelUsageFields: ChannelUsageFields{OriginalModel: "model001", ChannelMappedModel: "gpt-5.4", BillingModelSource: tc.source}}))
			require.NotNil(t, ledger.lastCmd)
			require.InDelta(t, tc.wantCost, ledger.lastCmd.BalanceCost, 1e-12, "public-card protection must preserve explicit opt-outs and ordinary Codex correction")
			require.NotNil(t, logs.lastLog.UpstreamResponseModel)
			require.Equal(t, "gpt-5.3-codex", *logs.lastLog.UpstreamResponseModel)
		})
	}
}

func codexPublicPriceFixture(source string, public ChannelModelPricing, billingSource string, mapping bool) (*OpenAIGatewayService, *APIKey, *openAIRecordUsageLogRepoStub, *openAIRecordUsageBillingRepoStub) {
	group := &Group{ID: 77, Platform: PlatformComposite, RateMultiplier: 1,
		BillingRatePolicy: domain.BillingRatePolicy{PlatformMultipliers: map[string]float64{PlatformOpenAI: 2},
			ModelMultipliers: []domain.BillingModelRate{{Platform: PlatformOpenAI, Model: "model001", Multiplier: 3}}}}
	channel := Channel{ID: 9, GroupIDs: []int64{77}, Status: StatusActive, BillingModelSource: billingSource,
		ModelPricing: []ChannelModelPricing{
			{Platform: PlatformOpenAI, Models: []string{"gpt-5.4"}, BillingMode: BillingModePerRequest, PerRequestPrice: billingFloat(4)},
			{Platform: PlatformOpenAI, Models: []string{"gpt-5.3-codex"}, BillingMode: BillingModePerRequest, PerRequestPrice: billingFloat(5)},
		}}
	if mapping {
		channel.ModelMapping = map[string]map[string]string{PlatformOpenAI: {"model001": "gpt-5.4"}}
	}
	if len(public.Models) > 0 {
		if source == PricingSourceGroup {
			group.ModelPricing = []ChannelModelPricing{public}
		} else {
			channel.ModelPricing = append(channel.ModelPricing, public)
		}
	}
	cache := populateChannelCache([]Channel{channel}, map[int64]string{77: PlatformComposite})
	channels := &ChannelService{frozenCache: cache}
	channels.cache.Store(cache)
	logs, ledger := &openAIRecordUsageLogRepoStub{inserted: true}, &openAIRecordUsageBillingRepoStub{}
	svc := newOpenAIRecordUsageServiceWithBillingRepoForTest(logs, ledger, &openAIRecordUsageUserRepoStub{}, &openAIRecordUsageSubRepoStub{}, nil)
	svc.channelService, svc.resolver = channels, NewModelPricingResolver(channels, svc.billingService)
	user := &User{ID: 22}
	return svc, &APIKey{ID: 11, Group: group, GroupID: &group.ID, User: user}, logs, ledger
}

func codexPublicPriceResult() *OpenAIForwardResult {
	return &OpenAIForwardResult{RequestID: "codex-public-offer", Model: "model001", BillingModel: "gpt-5.4", UpstreamModel: "gpt-5.4",
		UpstreamResponseModel: "gpt-5.3-codex", Usage: OpenAIUsage{InputTokens: 1, OutputTokens: 1}, Duration: time.Second}
}
