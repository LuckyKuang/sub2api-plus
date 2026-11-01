//go:build unit

package service

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// All concrete suppliers must charge an unknown successful text model using
// their own default system card. Expected bills are independent, literal sums
// for 1000 input + 1000 output tokens at the documented off-peak prices.
func TestPlatformPricingFallbackSettlement_AllDefaultsReachLedgerAndDurableEstimate(t *testing.T) {
	for _, tc := range []struct {
		platform, model string
		want            float64
	}{
		{"anthropic", "claude-opus-5-5", .024},
		{"openai", "gpt-6.1-sol", .012},
		{"gemini", "gemini-3.8-flash", .0045},
		{"antigravity", "gemini-3.8-flash", .0045},
		{"grok", "grok-4.6", .008},
		{"kimi", "kimi-k3", .018},
		{"zhipu", "glm-5.3", .0058},
		{"deepseek", "deepseek-v4.1-flash", .00075},
		{"minimax", "minimax-m3", .003},
		{"stepfun", "step-5-preview", .0037},
		{"opencode_go", "glm-5.3", .0058},
		{"typesafe", "jev-latest", .000042},
		{"command_code", "deepseek-v4.1-flash", .00075},
		{"cline", "deepseek-v4.1-flash", .00075},
	} {
		for _, groupPlatform := range []string{tc.platform, PlatformComposite} {
			for _, gateway := range []string{"generic", "openai"} {
				t.Run(tc.platform+"/"+groupPlatform+"/"+gateway, func(t *testing.T) {
					const requested = "unpublished-request-sku"
					group := &Group{ID: 77, Platform: groupPlatform, RateMultiplier: 1, Hydrated: true,
						ModelPricing: []ChannelModelPricing{{Platform: tc.platform, Models: []string{tc.model}, BillingMode: BillingModeToken, InputPrice: billingFloat(.1), OutputPrice: billingFloat(.2)}}}
					key := &APIKey{ID: 11, User: &User{ID: 22}, Group: group, GroupID: &group.ID}
					channel := Channel{ID: 9, Status: StatusActive, GroupIDs: []int64{group.ID}, BillingModelSource: BillingModelSourceRequested,
						ModelPricing: []ChannelModelPricing{{Platform: tc.platform, Models: []string{tc.model}, BillingMode: BillingModeToken, InputPrice: billingFloat(.3), OutputPrice: billingFloat(.4)}}}
					billing := platformFallbackTestBilling(nil)
					cache := populateChannelCache([]Channel{channel}, map[int64]string{group.ID: groupPlatform})
					channels := &ChannelService{frozenCache: cache}
					channels.cache.Store(cache)
					resolver := NewModelPricingResolver(channels, billing)
					at := time.Date(2026, 10, 25, 12, 0, 0, 0, time.UTC)
					ctx := withBillingPricingSnapshot(WithResolvedTargetPlatform(context.Background(), tc.platform), key, at, billing, channels, nil, requested)
					require.NoError(t, ValidateBillingPricingSnapshot(ctx))
					price, err := billing.GetModelPricingForPlatform(ctx, tc.platform, requested)
					require.NoError(t, err)
					require.Equal(t, tc.model, price.FallbackModel)
					require.Equal(t, tc.platform, price.FallbackPlatform)
					deps := inflightEstimateDeps{billing: billing, resolver: resolver}
					req := InflightEstimateRequest{Model: requested, BodyBytes: 4000, MaxTokens: 1000}
					amount, priced := deps.estimate(ctx, key, req)
					require.True(t, priced)
					require.InDelta(t, tc.want, amount, 1e-12)
					evidence, err := ExportBillingPricingSnapshot(ctx, "provider-wire-model")
					require.NoError(t, err)
					ctx, err = RestoreBillingPricingSnapshot(context.Background(), evidence)
					require.NoError(t, err)
					billing.frozenPricingFallbackModels = map[string]string{tc.platform: ""}
					channels.frozenCache = populateChannelCache(nil, nil)
					amount, priced = deps.estimate(ctx, key, req)
					require.True(t, priced)
					require.InDelta(t, tc.want, amount, 1e-12, "restored jobs must retain the accepted platform fallback")
					logs, ledger := &openAIRecordUsageLogRepoStub{inserted: true}, &openAIRecordUsageBillingRepoStub{}
					account := &Account{ID: 33, Platform: tc.platform, Type: AccountTypeAPIKey}
					fields := ChannelUsageFields{ChannelID: 9, OriginalModel: requested, ChannelMappedModel: requested, BillingModelSource: BillingModelSourceRequested}
					if gateway == "generic" {
						svc := newGatewayRecordUsageServiceWithBillingRepoForTest(logs, ledger, &openAIRecordUsageUserRepoStub{}, &openAIRecordUsageSubRepoStub{})
						svc.billingService, svc.channelService, svc.resolver = billing, channels, resolver
						input := &RecordUsageInput{Result: &ForwardResult{RequestID: "default-fallback", Model: requested, UpstreamModel: "provider-wire-model", UpstreamResponseModel: "gpt-6-luna", Usage: ClaudeUsage{InputTokens: 1000, OutputTokens: 1000}}, APIKey: key, User: key.User, Account: account, ChannelUsageFields: fields}
						require.InDelta(t, tc.want, svc.QuoteUsageCost(ctx, input), 1e-12)
						require.NoError(t, svc.RecordUsage(ctx, input))
					} else {
						svc := newOpenAIRecordUsageServiceWithBillingRepoForTest(logs, ledger, &openAIRecordUsageUserRepoStub{}, &openAIRecordUsageSubRepoStub{}, nil)
						svc.billingService, svc.channelService, svc.resolver = billing, channels, resolver
						require.NoError(t, svc.RecordUsage(ctx, &OpenAIRecordUsageInput{Result: &OpenAIForwardResult{RequestID: "default-fallback", Model: requested, BillingModel: requested, UpstreamModel: "provider-wire-model", UpstreamResponseModel: "gpt-6-luna", Usage: OpenAIUsage{InputTokens: 1000, OutputTokens: 1000}}, APIKey: key, User: key.User, Account: account, ChannelUsageFields: fields}))
					}
					require.Equal(t, 1, logs.calls)
					require.Equal(t, 1, ledger.calls)
					require.InDelta(t, tc.want, ledger.lastCmd.BalanceCost, 1e-12, "unknown successful models must not become free")
					require.InDelta(t, tc.want, logs.lastLog.TotalCost, 1e-12)
					require.InDelta(t, tc.want, logs.lastLog.ActualCost, 1e-12)
					require.Equal(t, requested, logs.lastLog.RequestedModel)
					require.Equal(t, "provider-wire-model", *logs.lastLog.UpstreamModel)
					require.Equal(t, "gpt-6-luna", *logs.lastLog.UpstreamResponseModel)
				})
			}
		}
	}
}
