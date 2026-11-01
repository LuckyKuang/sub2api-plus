//go:build unit

package service

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/config"
	"github.com/stretchr/testify/require"
)

// Business contract: request/channel-selected Sol prices survive a provider's
// Luna observation. Deliberately distinct fixture rates prevent Sol variants,
// retail overrides, and the reported Luna from accidentally producing equal bills.
func TestOpenAISelectedModelBilling_PreservesSelectedPriceAndAudit(t *testing.T) {
	cases := []struct {
		name, requested, mapped, wire, source, reported, retail string
		input, output                                           *float64
		perRequest                                              *float64
		conflict                                                bool
		want                                                    float64
	}{
		{name: "default mapped global Sol", requested: "gpt-6-luna", mapped: "gpt-6-sol", reported: "gpt-6-luna", want: .012},
		{name: "mapped global Sol", requested: "gpt-6-luna", mapped: "gpt-6-sol", source: BillingModelSourceChannelMapped, reported: "gpt-6-luna", want: .012},
		{name: "mapped channel Sol", requested: "gpt-6-luna", mapped: "gpt-6-sol", source: BillingModelSourceChannelMapped, reported: "gpt-6-luna", retail: PricingSourceChannel, input: billingFloat(5e-6), output: billingFloat(11e-6), want: .016},
		{name: "mapped group Sol", requested: "gpt-6-luna", mapped: "gpt-6-sol", source: BillingModelSourceChannelMapped, reported: "gpt-6-luna", retail: PricingSourceGroup, input: billingFloat(6e-6), output: billingFloat(12e-6), want: .018},
		{name: "mapped partial Sol", requested: "gpt-6-luna", mapped: "gpt-6-sol", source: BillingModelSourceChannelMapped, reported: "gpt-6-luna", retail: PricingSourceChannel, input: billingFloat(4e-6), want: .013},
		{name: "mapped empty card uses global", requested: "gpt-6-luna", mapped: "gpt-6-sol", source: BillingModelSourceChannelMapped, reported: "gpt-6-luna", retail: PricingSourceChannel, want: .012},
		{name: "mapped explicit free", requested: "gpt-6-luna", mapped: "gpt-6-sol", source: BillingModelSourceChannelMapped, reported: "gpt-6-luna", retail: PricingSourceChannel, input: billingFloat(0), output: billingFloat(0)},
		{name: "mapped per request", requested: "gpt-6-luna", mapped: "gpt-6-sol", source: BillingModelSourceChannelMapped, reported: "gpt-6-luna", retail: PricingSourceChannel, perRequest: billingFloat(.25), want: .25},
		{name: "requested global 6.1 Sol", requested: "gpt-6.1-sol", source: BillingModelSourceRequested, reported: "gpt-6-luna", want: .020},
		{name: "default unmapped 6.1 Sol", requested: "gpt-6.1-sol", source: BillingModelSourceChannelMapped, reported: "gpt-6-luna", want: .020},
		{name: "legacy empty source", requested: "gpt-6.1-sol", reported: "gpt-6-luna", want: .020},
		{name: "requested ignores mapping", requested: "gpt-6.1-sol", mapped: "gpt-6-sol", source: BillingModelSourceRequested, reported: "gpt-6-luna", want: .020},
		{name: "mapped Sol survives account mapping", requested: "gpt-6-luna", mapped: "gpt-6-sol", wire: "gpt-6.1-sol", source: BillingModelSourceChannelMapped, reported: "gpt-6-luna", want: .012},
		{name: "requested Sol survives account mapping", requested: "gpt-6.1-sol", wire: "gpt-6-sol", source: BillingModelSourceRequested, reported: "gpt-6-luna", want: .020},
		{name: "requested public channel price", requested: "gpt-6.1-sol", source: BillingModelSourceRequested, reported: "gpt-6-luna", retail: PricingSourceChannel, input: billingFloat(5e-6), output: billingFloat(11e-6), want: .016},
		{name: "expensive observation cannot raise mapped bill", requested: "gpt-6-luna", mapped: "gpt-6-sol", source: BillingModelSourceChannelMapped, reported: "gpt-6.1-sol", want: .012},
		{name: "conflicting observation", requested: "gpt-6.1-sol", source: BillingModelSourceRequested, reported: "gpt-6-luna", conflict: true, want: .020},
		{name: "unknown observation", requested: "gpt-6.1-sol", source: BillingModelSourceRequested, reported: "unpriced-response-model", want: .020},
		{name: "empty observation", requested: "gpt-6.1-sol", source: BillingModelSourceRequested, want: .020},
		{name: "future request uses platform fallback", requested: "gpt-6.2", source: BillingModelSourceRequested, reported: "gpt-6-luna", want: .020},
	}
	for _, tc := range cases {
		for _, accountType := range []string{AccountTypeOAuth, AccountTypeSetupToken, AccountTypeAPIKey} {
			for _, platform := range []string{PlatformOpenAI, PlatformComposite} {
				for _, stream := range []bool{false, true} {
					for _, durable := range []bool{false, true} {
						t.Run(fmt.Sprintf("%s/%s/%s/stream=%v/durable=%v", tc.name, accountType, platform, stream, durable), func(t *testing.T) {
							group := &Group{ID: 77, Platform: platform, RateMultiplier: 1, Hydrated: true}
							key := &APIKey{ID: 11, User: &User{ID: 22}, Group: group, GroupID: &group.ID}
							channel := Channel{ID: 9, Status: StatusActive, GroupIDs: []int64{group.ID}, BillingModelSource: tc.source}
							selected := tc.requested
							if tc.mapped != "" {
								channel.ModelMapping = map[string]map[string]string{PlatformOpenAI: {tc.requested: tc.mapped}}
								if tc.source != BillingModelSourceRequested {
									selected = tc.mapped
								}
							}
							if tc.retail != "" {
								card := ChannelModelPricing{Platform: PlatformOpenAI, Models: []string{selected}, BillingMode: BillingModeToken, InputPrice: tc.input, OutputPrice: tc.output}
								if tc.perRequest != nil {
									card.BillingMode = BillingModePerRequest
									card.PerRequestPrice = tc.perRequest
								}
								if tc.retail == PricingSourceGroup {
									group.ModelPricing = []ChannelModelPricing{card}
								} else {
									channel.ModelPricing = []ChannelModelPricing{card}
								}
							}
							cache := populateChannelCache([]Channel{channel}, map[int64]string{group.ID: platform})
							channels := &ChannelService{frozenCache: cache}
							channels.cache.Store(cache)
							cfg := &config.Config{}
							cfg.Default.RateMultiplier = 1
							billing := NewBillingService(cfg, newStubPricingServiceFromJSON(t, `{
        "gpt-6-luna":{"litellm_provider":"openai","input_cost_per_token":0.0000001,"output_cost_per_token":0.0000002},
        "gpt-6-sol":{"litellm_provider":"openai","input_cost_per_token":0.000003,"output_cost_per_token":0.000009},
        "gpt-6.1-sol":{"litellm_provider":"openai","input_cost_per_token":0.000007,"output_cost_per_token":0.000013}
       }`))
							resolver := NewModelPricingResolver(channels, billing)
							logs, ledger := &openAIRecordUsageLogRepoStub{inserted: true}, &openAIRecordUsageBillingRepoStub{}
							svc := newOpenAIRecordUsageServiceWithBillingRepoForTest(logs, ledger, &openAIRecordUsageUserRepoStub{}, &openAIRecordUsageSubRepoStub{}, nil)
							svc.cfg, svc.billingService, svc.channelService, svc.resolver = cfg, billing, channels, resolver
							account := &Account{ID: 33, Platform: PlatformOpenAI, Type: accountType}
							if tc.wire != "" {
								accountInput := tc.mapped
								if accountInput == "" {
									accountInput = tc.requested
								}
								account.Credentials = map[string]any{"model_mapping": map[string]any{accountInput: tc.wire}}
							}
							at := time.Date(2026, 10, 25, 12, 0, 0, 0, time.UTC)
							ctx := svc.WithBillingPricingSnapshot(WithResolvedTargetPlatform(context.Background(), PlatformOpenAI), key, at, tc.requested)
							require.NoError(t, ValidateBillingPricingSnapshot(ctx))
							if durable {
								evidence, err := ExportBillingPricingSnapshot(ctx, selected, tc.reported)
								require.NoError(t, err)
								ctx, err = RestoreBillingPricingSnapshot(context.Background(), evidence)
								require.NoError(t, err)
								// Accepted evidence must outlive later live pricing/configuration edits.
								billing.pricingService = nil
								billing.frozenPricingFallbackModels = map[string]string{PlatformOpenAI: ""}
								channels.frozenCache = populateChannelCache(nil, nil)
							}
							amount, priced := svc.EstimateInflightReservationForAccount(ctx, key, InflightEstimateRequest{Model: tc.requested, BodyBytes: 4000, MaxTokens: 1000}, account)
							require.True(t, priced)
							require.InDelta(t, tc.want, amount, 1e-12, "reservation must use the selected retail/system card")
							sent := tc.mapped
							if sent == "" {
								sent = tc.requested
							}
							if tc.wire != "" {
								sent = tc.wire
							}
							result := &OpenAIForwardResult{RequestID: "selected-billing", Model: tc.requested, BillingModel: selected, UpstreamModel: sent,
								UpstreamResponseModel: tc.reported, UpstreamResponseModelConflict: tc.conflict, Stream: stream,
								Usage: OpenAIUsage{InputTokens: 1000, OutputTokens: 1000}, Duration: time.Second}
							require.NoError(t, svc.RecordUsage(ctx, &OpenAIRecordUsageInput{PricingSnapshot: BillingPricingSnapshotFromContext(ctx), Result: result, APIKey: key, User: key.User, Account: account,
								ChannelUsageFields: ChannelUsageFields{ChannelID: 9, OriginalModel: tc.requested, ChannelMappedModel: firstNonEmpty(tc.mapped, tc.requested), BillingModelSource: tc.source}}))
							require.Equal(t, 1, ledger.calls)
							require.Equal(t, 1, logs.calls)
							require.InDelta(t, tc.want, ledger.lastCmd.BalanceCost, 1e-12, "ledger must charge the selected model, irrespective of upstream self-report")
							require.InDelta(t, tc.want, logs.lastLog.TotalCost, 1e-12)
							require.InDelta(t, tc.want, logs.lastLog.ActualCost, 1e-12)
							require.Equal(t, tc.requested, logs.lastLog.Model)
							require.Equal(t, tc.requested, logs.lastLog.RequestedModel)
							if sent != tc.requested {
								require.Equal(t, sent, *logs.lastLog.UpstreamModel)
							}
							if tc.reported != "" {
								require.NotNil(t, logs.lastLog.UpstreamResponseModel)
								require.Equal(t, tc.reported, *logs.lastLog.UpstreamResponseModel)
							}
							require.Equal(t, tc.requested, result.Model)
							require.Equal(t, selected, result.BillingModel)
							require.Equal(t, sent, result.UpstreamModel)
						})
					}
				}
			}
		}
	}
}
