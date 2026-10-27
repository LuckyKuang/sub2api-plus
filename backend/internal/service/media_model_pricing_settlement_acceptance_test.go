//go:build unit

package service

import (
	"context"
	"fmt"
	"math"
	"testing"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/domain"
	"github.com/LuckyKuang/sub2api-plus/internal/pkg/timezone"
	"github.com/stretchr/testify/require"
)

func mediaSettlementChannel(groupID int64, card ChannelModelPricing) *ChannelService {
	cache := populateChannelCache([]Channel{{ID: 1, Status: StatusActive, GroupIDs: []int64{groupID}, ModelPricing: []ChannelModelPricing{card}}}, map[int64]string{groupID: PlatformComposite})
	channel := &ChannelService{}
	channel.cache.Store(cache)
	return channel
}

func TestOpenAIVideoModelCardsSettleAcceptedRequestAndDurationUnits(t *testing.T) {
	for _, source := range []string{PricingSourceGroup, PricingSourceChannel} {
		for _, mode := range []BillingMode{BillingModePerRequest, BillingModeImage, BillingModeVideo} {
			for _, count := range []int{1, 2} {
				for _, peak := range []bool{false, true} {
					t.Run(fmt.Sprintf("%s/%s/count=%d/peak=%t", source, mode, count, peak), func(t *testing.T) {
						logs := &openAIRecordUsageLogRepoStub{inserted: true}
						ledger := &openAIRecordUsageBillingRepoStub{}
						svc := newOpenAIRecordUsageServiceWithBillingRepoForTest(logs, ledger, &openAIRecordUsageUserRepoStub{}, &openAIRecordUsageSubRepoStub{}, nil)
						const model = "media-model001"
						group := &Group{ID: 77, Platform: PlatformComposite, RateMultiplier: 1, Hydrated: true, BillingPolicyVersion: 9,
							SubscriptionType: SubscriptionTypeSubscription, PeakRateEnabled: true, PeakStart: "09:00", PeakEnd: "12:00", PeakRateMultiplier: 1.5,
							BillingRatePolicy: domain.BillingRatePolicy{PlatformMultipliers: map[string]float64{PlatformGrok: 2}, ModelMultipliers: []domain.BillingModelRate{{Platform: PlatformGrok, Model: model, Multiplier: 3}}}}
						card := ChannelModelPricing{Platform: PlatformGrok, Models: []string{model}, BillingMode: mode, PerRequestPrice: billingFloat(2)}
						if source == PricingSourceGroup {
							group.ModelPricing = []ChannelModelPricing{card}
						} else {
							svc.channelService = mediaSettlementChannel(group.ID, card)
						}
						svc.resolver = NewModelPricingResolver(svc.channelService, svc.billingService)
						user := &User{ID: 22}
						key := &APIKey{ID: 11, Group: group, GroupID: &group.ID, User: user, Quota: 1000}
						hour := 8
						if peak {
							hour = 10
						}
						at := time.Date(2026, 10, 20, hour, 0, 0, 0, timezone.Location())
						ctx := svc.WithBillingPricingSnapshot(WithResolvedTargetPlatform(context.Background(), PlatformGrok), key, at, model)
						// A request/image card sells one video for $2, independent of
						// its five-second duration. A video card sells each second.
						wantTotal, wantCharge := 2.0, 12.0
						if mode == BillingModeVideo {
							wantTotal, wantCharge = 10, 60
						}
						if count == 2 {
							wantTotal, wantCharge = wantTotal*2, wantCharge*2
						}
						if peak {
							wantCharge *= 1.5
						}
						amount, priced := (inflightEstimateDeps{billing: svc.billingService, resolver: svc.resolver}).estimate(ctx, key, InflightEstimateRequest{Model: model, Kind: InflightEstimateVideo, Units: count, VideoResolution: "480p", VideoDurationSeconds: 5})
						require.True(t, priced)
						require.InDelta(t, wantCharge, amount, 1e-12, "admission must use the selected card's quantity")
						err := svc.RecordUsage(ctx, &OpenAIRecordUsageInput{Result: &OpenAIForwardResult{RequestID: "media-card-video", Model: model, BillingModel: model, VideoCount: count, VideoDurationSeconds: 5, VideoResolution: "480p"}, APIKey: key, User: user, Account: &Account{ID: 33, Platform: PlatformGrok}, Subscription: &UserSubscription{ID: 44}, OriginalModel: model, APIKeyService: &openAIRecordUsageAPIKeyQuotaStub{}})
						require.NoError(t, err)
						require.NotNil(t, logs.lastLog)
						require.InDelta(t, wantTotal, logs.lastLog.TotalCost, 1e-12)
						require.InDelta(t, wantCharge, logs.lastLog.ActualCost, 1e-12)
						require.Equal(t, string(BillingModeVideo), *logs.lastLog.BillingMode)
						require.NotNil(t, ledger.lastCmd)
						require.InDelta(t, wantCharge, ledger.lastCmd.SubscriptionCost, 1e-12)
						require.InDelta(t, wantCharge, ledger.lastCmd.APIKeyQuotaCost, 1e-12)
						require.Zero(t, ledger.lastCmd.BalanceCost)
					})
				}
			}
		}
	}
}

func TestOpenAIVideoModelCardsExplicitZeroCannotFallBackToPaidNativePrice(t *testing.T) {
	for _, source := range []string{PricingSourceGroup, PricingSourceChannel} {
		for _, mode := range []BillingMode{BillingModePerRequest, BillingModeImage, BillingModeVideo} {
			t.Run(source+"/"+string(mode), func(t *testing.T) {
				const model = "grok-imagine-video"
				logs := &openAIRecordUsageLogRepoStub{inserted: true}
				ledger := &openAIRecordUsageBillingRepoStub{}
				users := &openAIRecordUsageUserRepoStub{}
				subs := &openAIRecordUsageSubRepoStub{}
				quota := &openAIRecordUsageAPIKeyQuotaStub{}
				svc := newOpenAIRecordUsageServiceWithBillingRepoForTest(logs, ledger, users, subs, nil)
				group := &Group{ID: 77, Platform: PlatformComposite, RateMultiplier: 1, Hydrated: true,
					SubscriptionType: SubscriptionTypeSubscription, PeakRateEnabled: true, PeakStart: "09:00", PeakEnd: "12:00", PeakRateMultiplier: 1.5,
					BillingRatePolicy: domain.BillingRatePolicy{PlatformMultipliers: map[string]float64{PlatformGrok: 2}, ModelMultipliers: []domain.BillingModelRate{{Platform: PlatformGrok, Model: model, Multiplier: 3}}}}
				card := ChannelModelPricing{Platform: PlatformGrok, Models: []string{model}, BillingMode: mode, PerRequestPrice: billingFloat(0)}
				if source == PricingSourceGroup {
					group.ModelPricing = []ChannelModelPricing{card}
					// A group model card wins over this paid native fallback.
					// Channel cards retain their existing precedence below flat
					// group prices and instead have the paid native model default.
					group.VideoPrice480P = billingFloat(5)
				} else {
					svc.channelService = mediaSettlementChannel(group.ID, card)
				}
				svc.resolver = NewModelPricingResolver(svc.channelService, svc.billingService)
				user := &User{ID: 22}
				key := &APIKey{ID: 11, Group: group, GroupID: &group.ID, User: user, Quota: 100, RateLimit5h: 100}
				at := time.Date(2026, 10, 20, 10, 0, 0, 0, timezone.Location())
				ctx := svc.WithBillingPricingSnapshot(WithResolvedTargetPlatform(context.Background(), PlatformGrok), key, at, model)
				amount, priced := (inflightEstimateDeps{billing: svc.billingService, resolver: svc.resolver}).estimate(ctx, key, InflightEstimateRequest{Model: model, Kind: InflightEstimateVideo, Units: 1, VideoResolution: "480p", VideoDurationSeconds: 5})
				require.True(t, priced, "an explicit zero is known pricing")
				require.Zero(t, amount, "$0 remains free under platform2 × model3 × peak1.5")
				err := svc.RecordUsage(ctx, &OpenAIRecordUsageInput{Result: &OpenAIForwardResult{RequestID: "free-native-video-card", Model: model, BillingModel: model, VideoCount: 1, VideoDurationSeconds: 5, VideoResolution: "480p"}, APIKey: key, User: user, Account: &Account{ID: 33, Platform: PlatformGrok}, Subscription: &UserSubscription{ID: 44}, OriginalModel: model, APIKeyService: quota})
				require.NoError(t, err)
				require.NotNil(t, logs.lastLog, "free successful usage must remain visible")
				require.Zero(t, logs.lastLog.TotalCost)
				require.Zero(t, logs.lastLog.ActualCost)
				if command := ledger.lastCmd; command != nil {
					require.Zero(t, command.BalanceCost)
					require.Zero(t, command.SubscriptionCost)
					require.Zero(t, command.APIKeyQuotaCost)
					require.Zero(t, command.APIKeyRateLimitCost)
					require.Zero(t, command.AccountQuotaCost)
				}
				require.Zero(t, users.deductCalls)
				require.Zero(t, subs.incrementCalls)
				require.Zero(t, quota.quotaCalls)
				require.Zero(t, quota.rateLimitCalls)
			})
		}
	}
}

func TestNativeMediaSelectedUnpricedCardsCannotBecomeSuccessfulDefaultOrFreeSettlements(t *testing.T) {
	for _, gateway := range []string{"generic", "openai"} {
		for _, source := range []string{PricingSourceGroup, PricingSourceChannel} {
			for _, media := range []string{"audio", "image", "video"} {
				if gateway == "generic" && media == "video" {
					continue // Generic ForwardResult has no native video settlement.
				}
				modes := []BillingMode{BillingModePerRequest}
				if media == "image" {
					modes = append(modes, BillingModeImage)
				}
				if media == "video" {
					modes = append(modes, BillingModeImage, BillingModeVideo)
				}
				for _, mode := range modes {
					t.Run(fmt.Sprintf("%s/%s/%s/%s", gateway, source, media, mode), func(t *testing.T) {
						const model = "unpriced-native-media001"
						logs := &openAIRecordUsageLogRepoStub{inserted: true}
						ledger := &openAIRecordUsageBillingRepoStub{}
						users := &openAIRecordUsageUserRepoStub{}
						subs := &openAIRecordUsageSubRepoStub{}
						quota := &openAIRecordUsageAPIKeyQuotaStub{}
						group := &Group{ID: 77, Platform: PlatformComposite, RateMultiplier: 1, Hydrated: true}
						// The only price needs context >= 100 and a different size.
						// Native media here has context 0: it has no applicable price.
						card := ChannelModelPricing{Platform: PlatformGrok, Models: []string{model}, BillingMode: mode, Intervals: []PricingInterval{{MinTokens: 100, TierLabel: "other", PerRequestPrice: billingFloat(2)}}}
						var channel *ChannelService
						if source == PricingSourceGroup {
							group.ModelPricing = []ChannelModelPricing{card}
						} else {
							channel = mediaSettlementChannel(group.ID, card)
						}
						user := &User{ID: 22}
						key := &APIKey{ID: 11, Group: group, GroupID: &group.ID, User: user, Quota: 100, RateLimit5h: 100}
						account := &Account{ID: 33, Platform: PlatformGrok}
						ctx := WithResolvedTargetPlatform(context.Background(), PlatformGrok)
						var err error
						if gateway == "generic" {
							svc := newGatewayRecordUsageServiceWithBillingRepoForTest(logs, ledger, users, subs)
							svc.channelService, svc.resolver = channel, NewModelPricingResolver(channel, svc.billingService)
							ctx = svc.WithBillingPricingSnapshot(ctx, key, time.Now(), model)
							result := &ForwardResult{RequestID: "unpriced-native", Model: model}
							if media == "image" {
								result.ImageCount, result.ImageSize = 1, "1K"
							} else {
								result.AudioUsage = &AudioUsage{Mode: "tts", DurationOrUnits: 1}
							}
							input := &RecordUsageInput{Result: result, APIKey: key, User: user, Account: account, OriginalModel: model, APIKeyService: quota}
							require.True(t, math.IsNaN(svc.QuoteUsageCost(ctx, input)), "selected unpriced media must not quote a fallback cost")
							err = svc.RecordUsage(ctx, input)
						} else {
							svc := newOpenAIRecordUsageServiceWithBillingRepoForTest(logs, ledger, users, subs, nil)
							svc.channelService, svc.resolver = channel, NewModelPricingResolver(channel, svc.billingService)
							ctx = svc.WithBillingPricingSnapshot(ctx, key, time.Now(), model)
							result := &OpenAIForwardResult{RequestID: "unpriced-native", Model: model, BillingModel: model}
							switch media {
							case "image":
								result.ImageCount, result.ImageSize = 1, "1K"
							case "video":
								result.VideoCount, result.VideoDurationSeconds, result.VideoResolution = 1, 5, "480p"
							default:
								result.AudioUsage = &AudioUsage{Mode: "tts", DurationOrUnits: 1}
							}
							err = svc.RecordUsage(ctx, &OpenAIRecordUsageInput{Result: result, APIKey: key, User: user, Account: account, OriginalModel: model, APIKeyService: quota})
						}
						require.ErrorIs(t, err, ErrModelPricingUnavailable)
						require.Zero(t, logs.calls, "unavailable cost must not create a successful usage row")
						require.Zero(t, ledger.calls, "unavailable cost must not enter the ledger")
						require.Zero(t, users.deductCalls)
						require.Zero(t, subs.incrementCalls)
						require.Zero(t, quota.quotaCalls)
						require.Zero(t, quota.rateLimitCalls)
					})
				}
			}
		}
	}
}
