//go:build unit

package service

import (
	"context"
	"testing"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/domain"
	"github.com/stretchr/testify/require"
)

// The image estimator cannot distinguish the eventual output size. A priced
// reservation therefore requires all three tiers and reserves their maximum.
func TestImageCountBillingRequiresEveryPotentialSizeBeforeReservation(t *testing.T) {
	for _, platform := range []string{PlatformOpenAI, PlatformGrok} {
		for _, durable := range []bool{false, true} {
			for _, tc := range []struct {
				name   string
				prices [3]*float64
				priced bool
				want   float64
			}{
				{"only 1K configured", [3]*float64{billingFloat(.5), nil, nil}, false, 0},
				{"missing 1K", [3]*float64{nil, billingFloat(.8), billingFloat(.3)}, false, 0},
				{"missing 2K", [3]*float64{billingFloat(.1), nil, billingFloat(.3)}, false, 0},
				{"missing 4K", [3]*float64{billingFloat(.1), billingFloat(.8), nil}, false, 0},
				{"all sizes known reserve highest", [3]*float64{billingFloat(.1), billingFloat(.8), billingFloat(.3)}, true, 1.6},
				{"explicit zero size is known", [3]*float64{billingFloat(.5), billingFloat(0), billingFloat(.25)}, true, 1},
				{"all explicit zero sizes are known", [3]*float64{billingFloat(0), billingFloat(0), billingFloat(0)}, true, 0},
			} {
				t.Run(platform+"/"+map[bool]string{false: "request", true: "durable"}[durable]+"/"+tc.name, func(t *testing.T) {
					logs := &openAIRecordUsageLogRepoStub{inserted: true}
					ledger := &openAIRecordUsageBillingRepoStub{}
					users := &openAIRecordUsageUserRepoStub{}
					subs := &openAIRecordUsageSubRepoStub{}
					quota := &openAIRecordUsageAPIKeyQuotaStub{}
					svc := newGatewayRecordUsageServiceWithBillingRepoForTest(logs, ledger, users, subs)
					group := imageCountPlusGroup()
					group.BillingRatePolicy = domain.BillingRatePolicy{}
					group.ModelPricing[0].Platform = platform
					group.ImagePrice1K, group.ImagePrice2K, group.ImagePrice4K = tc.prices[0], tc.prices[1], tc.prices[2]
					user := &User{ID: 22}
					key := &APIKey{ID: 11, GroupID: &group.ID, Group: group, User: user, Quota: 100}
					ctx := svc.WithBillingPricingSnapshot(WithResolvedTargetPlatform(context.Background(), platform), key, time.Now(), "retail-image")
					if durable {
						raw, err := ExportBillingPricingSnapshot(ctx, "retail-image")
						require.NoError(t, err)
						ctx, err = RestoreBillingPricingSnapshot(context.Background(), raw)
						require.NoError(t, err)
					}
					// Updating today's group must neither authorize missing captured
					// prices nor change the maximum promised to accepted requests.
					group.ImagePrice1K, group.ImagePrice2K, group.ImagePrice4K = billingFloat(99), billingFloat(99), billingFloat(99)
					amount, priced := svc.inflightEstimateDeps().estimate(ctx, key, InflightEstimateRequest{Model: "retail-image", Kind: InflightEstimateImage, Units: 2})
					require.Equal(t, tc.priced, priced)
					require.InDelta(t, tc.want, amount, 1e-12)
					if !tc.priced {
						missing := "1K"
						if tc.prices[1] == nil {
							missing = "2K"
						} else if tc.prices[2] == nil {
							missing = "4K"
						}
						input := &RecordUsageInput{Result: &ForwardResult{RequestID: "missing-size", Model: "retail-image", ImageEndpoint: true, ImageCount: 2, ImageSize: missing}, APIKey: key, User: user, Account: &Account{ID: 33, Platform: platform}, OriginalModel: "retail-image", APIKeyService: quota}
						require.ErrorIs(t, svc.RecordUsage(ctx, input), ErrModelPricingUnavailable)
						require.Zero(t, logs.calls)
						require.Zero(t, ledger.calls)
						require.Zero(t, users.deductCalls)
						require.Zero(t, subs.incrementCalls)
						require.Zero(t, quota.quotaCalls)
						require.Zero(t, quota.rateLimitCalls)
					} else {
						// 2K is the highest-priced size in the nonzero success
						// fixture; explicit-zero cases remain priced settlement.
						size := "2K"
						if tc.name == "explicit zero size is known" {
							size = "1K"
						}
						input := &RecordUsageInput{Result: &ForwardResult{RequestID: "known-size", Model: "retail-image", ImageEndpoint: true, ImageCount: 2, ImageSize: size}, APIKey: key, User: user, Account: &Account{ID: 33, Platform: platform}, OriginalModel: "retail-image", APIKeyService: quota}
						require.InDelta(t, tc.want, svc.QuoteUsageCost(ctx, input), 1e-12)
						require.NoError(t, svc.RecordUsage(ctx, input))
						require.NotNil(t, logs.lastLog)
						require.InDelta(t, tc.want, logs.lastLog.TotalCost, 1e-12)
					}
				})
			}
		}
	}
}
