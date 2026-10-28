//go:build unit

package service

import (
	"context"
	"encoding/json"
	"github.com/LuckyKuang/sub2api-plus/internal/config"
	"math"
	"testing"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/domain"
	"github.com/stretchr/testify/require"
)

// These amounts follow the retail contract, independent of routing and live
// account changes: two images at $0.50, platform factor 2 and public-model factor 3.
func TestImageCountBillingPlusPinsQuoteReservationAndSettlement(t *testing.T) {
	for _, durable := range []bool{false, true} {
		t.Run(map[bool]string{false: "inflight", true: "durable"}[durable], func(t *testing.T) {
			logs := &openAIRecordUsageLogRepoStub{inserted: true}
			ledger := &openAIRecordUsageBillingRepoStub{}
			svc := newGatewayRecordUsageServiceWithBillingRepoForTest(logs, ledger, &openAIRecordUsageUserRepoStub{}, &openAIRecordUsageSubRepoStub{})
			group := imageCountPlusGroup()
			user := &User{ID: 22}
			key := &APIKey{ID: 11, GroupID: &group.ID, Group: group, User: user, Quota: 100}
			ctx := svc.WithBillingPricingSnapshot(WithResolvedTargetPlatform(context.Background(), PlatformOpenAI), key, time.Now(), "retail-image")
			if durable {
				raw, err := ExportBillingPricingSnapshot(ctx, "retail-image")
				require.NoError(t, err)
				ctx, err = RestoreBillingPricingSnapshot(context.Background(), raw)
				require.NoError(t, err)
			}
			group.ImageCountBilling = false
			*group.ImagePrice1K, *group.ImagePrice2K, *group.ImagePrice4K = 99, 99, 99
			group.BillingRatePolicy.PlatformMultipliers[PlatformOpenAI] = 100
			group.ModelPricing[0].InputPrice = billingFloat(100)
			input := &RecordUsageInput{Result: &ForwardResult{RequestID: "pinned-image", Model: "retail-image", ImageCount: 2, ImageSize: "1K", ImageEndpoint: true, Usage: ClaudeUsage{InputTokens: 1000000}}, APIKey: key, User: user, Account: &Account{ID: 33, Platform: PlatformOpenAI}, OriginalModel: "retail-image", APIKeyService: &openAIRecordUsageAPIKeyQuotaStub{}}
			require.InDelta(t, 6.0, svc.QuoteUsageCost(ctx, input), 1e-12)
			reserved, priced := svc.inflightEstimateDeps().estimate(ctx, key, InflightEstimateRequest{Model: "retail-image", Kind: InflightEstimateImage, Units: 2})
			require.True(t, priced)
			require.InDelta(t, 6.0, reserved, 1e-12)
			require.NoError(t, svc.RecordUsage(ctx, input))
			require.NotNil(t, ledger.lastCmd)
			require.InDelta(t, 6.0, ledger.lastCmd.APIKeyQuotaCost, 1e-12)
			require.InDelta(t, 1.0, logs.lastLog.TotalCost, 1e-12)
		})
	}
}

func imageCountPlusGroup() *Group {
	return &Group{ID: 77, Platform: PlatformComposite, RateMultiplier: 1, ImageCountBilling: true, AllowImageGeneration: true,
		ImagePrice1K: billingFloat(.5), ImagePrice2K: billingFloat(.5), ImagePrice4K: billingFloat(.5),
		BillingRatePolicy: domain.BillingRatePolicy{PlatformMultipliers: map[string]float64{PlatformOpenAI: 2}, ModelMultipliers: []domain.BillingModelRate{{Platform: PlatformOpenAI, Model: "retail-image", Multiplier: 3}}},
		ModelPricing:      []ChannelModelPricing{{Platform: PlatformOpenAI, Models: []string{"retail-image"}, BillingMode: BillingModeToken, InputPrice: billingFloat(2e-6), OutputPrice: billingFloat(4e-6)}}}
}

func TestImageCountBillingPlusMixedRequestRetainsTokenCharge(t *testing.T) {
	logs := &openAIRecordUsageLogRepoStub{inserted: true}
	ledger := &openAIRecordUsageBillingRepoStub{}
	svc := newGatewayRecordUsageServiceWithBillingRepoForTest(logs, ledger, &openAIRecordUsageUserRepoStub{}, &openAIRecordUsageSubRepoStub{})
	group := imageCountPlusGroup()
	user := &User{ID: 22}
	key := &APIKey{ID: 11, GroupID: &group.ID, Group: group, User: user, Quota: 100}
	ctx := svc.WithBillingPricingSnapshot(WithResolvedTargetPlatform(context.Background(), PlatformOpenAI), key, time.Now(), "retail-image")
	input := &RecordUsageInput{Result: &ForwardResult{RequestID: "mixed-image", Model: "retail-image", ImageCount: 2, ImageEndpoint: false, Usage: ClaudeUsage{InputTokens: 1000000}}, APIKey: key, User: user, Account: &Account{ID: 33, Platform: PlatformOpenAI}, OriginalModel: "retail-image", APIKeyService: &openAIRecordUsageAPIKeyQuotaStub{}}
	require.InDelta(t, 12.0, svc.QuoteUsageCost(ctx, input), 1e-12, "one million input tokens at $2/MTok times platform2/public3")
	require.NoError(t, svc.RecordUsage(ctx, input))
	require.InDelta(t, 12.0, ledger.lastCmd.APIKeyQuotaCost, 1e-12)
}

func TestImageCountBillingPlusUnknownImagePriceCannotWriteFinances(t *testing.T) {
	logs := &openAIRecordUsageLogRepoStub{inserted: true}
	ledger := &openAIRecordUsageBillingRepoStub{}
	users := &openAIRecordUsageUserRepoStub{}
	subs := &openAIRecordUsageSubRepoStub{}
	quota := &openAIRecordUsageAPIKeyQuotaStub{}
	svc := newGatewayRecordUsageServiceWithBillingRepoForTest(logs, ledger, users, subs)
	group := imageCountPlusGroup()
	group.ImagePrice1K, group.ImagePrice2K, group.ImagePrice4K = nil, nil, nil
	user := &User{ID: 22}
	key := &APIKey{ID: 11, GroupID: &group.ID, Group: group, User: user, Quota: 100}
	ctx := svc.WithBillingPricingSnapshot(WithResolvedTargetPlatform(context.Background(), PlatformOpenAI), key, time.Now(), "retail-image")
	input := &RecordUsageInput{Result: &ForwardResult{RequestID: "unknown-image", Model: "retail-image", ImageEndpoint: true, ImageCount: 2, ImageSize: "1K", Usage: ClaudeUsage{InputTokens: 1000000}}, APIKey: key, User: user, Account: &Account{ID: 33, Platform: PlatformOpenAI}, OriginalModel: "retail-image", APIKeyService: quota}
	require.True(t, math.IsNaN(svc.QuoteUsageCost(ctx, input)), "a known token price is not evidence of an image unit price")
	_, priced := svc.inflightEstimateDeps().estimate(ctx, key, InflightEstimateRequest{Model: "retail-image", Kind: InflightEstimateImage, Units: 2})
	require.False(t, priced)
	require.ErrorIs(t, svc.RecordUsage(ctx, input), ErrModelPricingUnavailable)
	require.Zero(t, logs.calls)
	require.Zero(t, ledger.calls)
	require.Zero(t, users.deductCalls)
	require.Zero(t, subs.incrementCalls)
	require.Zero(t, quota.quotaCalls)
	require.Zero(t, quota.rateLimitCalls)
}

func TestImageCountBillingPlusDurableImageEvidenceAndLegacyDefault(t *testing.T) {
	billing := NewBillingService(&config.Config{}, &PricingService{pricingData: map[string]*LiteLLMModelPricing{"catalog-image": {OutputCostPerImage: .25, TokenPricingAbsent: true}}})
	group := imageCountPlusGroup()
	group.ImagePrice1K, group.ImagePrice2K, group.ImagePrice4K = nil, nil, nil
	key := &APIKey{GroupID: &group.ID, Group: group, User: &User{ID: 22}}
	ctx := withBillingPricingSnapshot(WithResolvedTargetPlatform(context.Background(), PlatformOpenAI), key, time.Now(), billing, nil, nil, "catalog-image")
	raw, err := ExportBillingPricingSnapshot(ctx, "catalog-image", "unknown-image")
	require.NoError(t, err)
	restored, err := RestoreBillingPricingSnapshot(context.Background(), raw)
	require.NoError(t, err)
	frozen := BillingPricingSnapshotFromContext(restored).billing
	require.True(t, frozen.hasIdentifiedImageUnitPrice("catalog-image", "1K", nil))
	require.False(t, frozen.hasIdentifiedImageUnitPrice("unknown-image", "1K", nil), "legacy $0.134 fallback is not price evidence")
	var stored map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(raw, &stored))
	delete(stored, "identified_images")
	var oldGroup map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(stored["group"], &oldGroup))
	delete(oldGroup, "ImageCountBilling")
	stored["group"], err = json.Marshal(oldGroup)
	require.NoError(t, err)
	legacyRaw, err := json.Marshal(stored)
	require.NoError(t, err)
	legacy, err := RestoreBillingPricingSnapshot(context.Background(), legacyRaw)
	require.NoError(t, err)
	old := BillingPricingSnapshotFromContext(legacy)
	require.False(t, old.group.ImageCountBilling, "old snapshots default to the original token-aware behavior")
	require.False(t, old.billing.hasIdentifiedImageUnitPrice("catalog-image", "1K", nil), "old snapshot cannot recover price evidence by looking at current catalogs")
}

// Two returned images at the >=1000-token context tier ($0.30 each) cost
// $0.60 on each engine and price source, regardless of the force-image switch.
func TestImageCountBillingPlusContextTierAcrossEnginesAndSources(t *testing.T) {
	for _, source := range []string{"group", "channel"} {
		for _, engine := range []string{"generic", "openai"} {
			for _, enabled := range []bool{false, true} {
				t.Run(source+"/"+engine+"/"+map[bool]string{false: "off", true: "on"}[enabled], func(t *testing.T) {
					groupID := int64(906)
					model := "image-context-tier"
					resolver := newOpenAITokenImageChannelPricingResolverForTest(t, groupID, model)
					ceiling := 1000
					card := ChannelModelPricing{Models: []string{model}, BillingMode: BillingModePerRequest, Intervals: []PricingInterval{
						{MinTokens: 0, MaxTokens: &ceiling, PerRequestPrice: billingFloat(.1)},
						{MinTokens: 1000, PerRequestPrice: billingFloat(.3)},
					}}
					group := &Group{ID: groupID, Platform: PlatformComposite, RateMultiplier: 1, ImageCountBilling: enabled, AllowImageGeneration: true, Hydrated: true}
					if source == "group" {
						group.ModelPricing = []ChannelModelPricing{card}
					} else {
						cache, ok := resolver.channelService.cache.Load().(*channelCache)
						require.True(t, ok)
						cache.pricingByGroupModel[channelModelKey{groupID: groupID, model: model}] = &card
					}
					key := &APIKey{GroupID: &groupID, Group: group}
					var cost *CostBreakdown
					if engine == "generic" {
						svc := &GatewayService{billingService: resolver.billingService, resolver: resolver}
						cost = svc.calculateRecordUsageCost(context.Background(), &ForwardResult{Model: model, ImageCount: 2, ImageEndpoint: true, ImageSize: "1K", Usage: ClaudeUsage{InputTokens: 5000, OutputTokens: 100}}, key, model, 1, 1, time.Time{})
					} else {
						svc := &OpenAIGatewayService{billingService: resolver.billingService, resolver: resolver}
						cost = svc.calculateOpenAIImageCost(context.Background(), model, key, &OpenAIForwardResult{Model: model, ImageCount: 2, ImageEndpoint: true, ImageSize: "1K", Usage: OpenAIUsage{InputTokens: 5000, OutputTokens: 100}}, 1)
					}
					require.NoError(t, validateUsageCost(cost))
					require.InDelta(t, .6, cost.TotalCost, 1e-12)
					require.InDelta(t, .6, cost.ActualCost, 1e-12)
				})
			}
		}
	}
}
