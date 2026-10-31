//go:build unit

package service

import (
	"context"
	"testing"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/config"
	"github.com/stretchr/testify/require"
)

// Removing implicit Codex correction must retain the administrator's explicit
// response_model choice and its price protections, for every credential type.
func TestOpenAIGatewayServiceRecordUsage_CodexExplicitResponseBillingGuards(t *testing.T) {
	for _, accountType := range []string{AccountTypeOAuth, AccountTypeSetupToken, AccountTypeAPIKey} {
		for _, tc := range []struct {
			name, requested, reported              string
			conflict, free, retail, responseRetail bool
			want                                   float64
		}{
			{name: "explicit cheaper response", requested: "gpt-6.1-sol", reported: "gpt-6-luna", want: .0003},
			{name: "pricier response rejected", requested: "gpt-6-luna", reported: "gpt-6.1-sol", want: .0003},
			{name: "conflicting response rejected", requested: "gpt-6.1-sol", reported: "gpt-6-luna", conflict: true, want: .020},
			{name: "unidentified response rejected", requested: "gpt-6.1-sol", reported: "unpriced-response-model", want: .020},
			{name: "empty response retains baseline", requested: "gpt-6.1-sol", want: .020},
			{name: "global response cannot bypass retail", requested: "gpt-6.1-sol", reported: "gpt-6-luna", retail: true, want: .020},
			{name: "cheaper response retail may apply", requested: "gpt-6.1-sol", reported: "gpt-6-luna", retail: true, responseRetail: true, want: .001},
			{name: "free response cannot erase paid baseline", requested: "gpt-6.1-sol", reported: "gpt-6-luna", free: true, want: .020},
		} {
			t.Run(accountType+"/"+tc.name, func(t *testing.T) {
				logs, ledger := &openAIRecordUsageLogRepoStub{inserted: true}, &openAIRecordUsageBillingRepoStub{}
				svc := newOpenAIRecordUsageServiceWithBillingRepoForTest(logs, ledger, &openAIRecordUsageUserRepoStub{}, &openAIRecordUsageSubRepoStub{}, nil)
				cfg := &config.Config{}
				cfg.Default.RateMultiplier = 1
				svc.cfg = cfg
				raw := `{"gpt-6.1-sol":{"litellm_provider":"openai","input_cost_per_token":0.000007,"output_cost_per_token":0.000013},"gpt-6-luna":{"litellm_provider":"openai","input_cost_per_token":0.0000001,"output_cost_per_token":0.0000002}}`
				if tc.free {
					raw = `{"gpt-6.1-sol":{"litellm_provider":"openai","input_cost_per_token":0.000007,"output_cost_per_token":0.000013},"gpt-6-luna":{"litellm_provider":"openai","input_cost_per_token":0,"output_cost_per_token":0}}`
				}
				svc.billingService = NewBillingService(cfg, newStubPricingServiceFromJSON(t, raw))
				key := &APIKey{ID: 11}
				if tc.retail {
					group := &Group{ID: 77, Platform: PlatformOpenAI, RateMultiplier: 1}
					key.Group, key.GroupID = group, &group.ID
					cards := []ChannelModelPricing{{Platform: PlatformOpenAI, Models: []string{tc.requested}, BillingMode: BillingModeToken, InputPrice: billingFloat(7e-6), OutputPrice: billingFloat(13e-6)}}
					if tc.responseRetail {
						cards = append(cards, ChannelModelPricing{Platform: PlatformOpenAI, Models: []string{tc.reported}, BillingMode: BillingModeToken, InputPrice: billingFloat(.5e-6), OutputPrice: billingFloat(.5e-6)})
					}
					cache := populateChannelCache([]Channel{{ID: 9, Status: StatusActive, GroupIDs: []int64{group.ID}, BillingModelSource: BillingModelSourceResponse, ModelPricing: cards}}, map[int64]string{group.ID: PlatformOpenAI})
					svc.channelService = &ChannelService{frozenCache: cache}
					svc.channelService.cache.Store(cache)
					svc.resolver = NewModelPricingResolver(svc.channelService, svc.billingService)
				}
				require.NoError(t, svc.RecordUsage(context.Background(), &OpenAIRecordUsageInput{
					Result: &OpenAIForwardResult{RequestID: "explicit-response", Model: tc.requested, UpstreamModel: tc.requested, UpstreamResponseModel: tc.reported, UpstreamResponseModelConflict: tc.conflict, Usage: OpenAIUsage{InputTokens: 1000, OutputTokens: 1000}, Duration: time.Second},
					APIKey: key, User: &User{ID: 22}, Account: &Account{ID: 33, Platform: PlatformOpenAI, Type: accountType},
					ChannelUsageFields: ChannelUsageFields{OriginalModel: tc.requested, BillingModelSource: BillingModelSourceResponse}}))
				require.Equal(t, 1, ledger.calls)
				require.InDelta(t, tc.want, ledger.lastCmd.BalanceCost, 1e-12)
				require.InDelta(t, tc.want, logs.lastLog.ActualCost, 1e-12)
				require.Equal(t, tc.requested, logs.lastLog.RequestedModel)
				if tc.reported != "" {
					require.Equal(t, tc.reported, *logs.lastLog.UpstreamResponseModel)
				}
			})
		}
	}
}
