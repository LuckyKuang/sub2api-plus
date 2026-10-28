//go:build unit

package service

import (
	"context"
	"testing"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/config"
	"github.com/stretchr/testify/require"
)

func TestPlatformPricingFallbackSettlement_GenericAndOpenAIUseSameSystemFallbackAsInflight(t *testing.T) {
	const publicModel = "unknown-public"
	const upstreamModel = "provider-wire-model"
	for _, gateway := range []string{"generic", "openai"} {
		for _, tc := range []struct {
			name       string
			input      *float64
			output     *float64
			inputCost  float64
			outputCost float64
			total      float64
		}{
			{name: "platform system fallback", inputCost: .0025, outputCost: .015, total: .0175},
			{name: "partial group input inherits fallback output", input: billingFloat(2e-6), inputCost: .002, outputCost: .015, total: .017},
			{name: "explicit free group card", input: billingFloat(0), output: billingFloat(0), total: 0},
		} {
			t.Run(gateway+"/"+tc.name, func(t *testing.T) {
				logs := &openAIRecordUsageLogRepoStub{inserted: true}
				ledger := &openAIRecordUsageBillingRepoStub{}
				user := &User{ID: 22}
				group := &Group{
					ID:             77,
					Platform:       PlatformOpenAI,
					Status:         StatusActive,
					Hydrated:       true,
					RateMultiplier: 1,
					ModelAllowlist: GroupModelAllowlist{Enabled: true, Models: []string{publicModel}},
					// These are retail prices for another request SKU. They must
					// never supply the platform fallback's system unit prices.
					ModelPricing: []ChannelModelPricing{{
						Platform: PlatformOpenAI, Models: []string{"gpt-5.4"}, BillingMode: BillingModeToken,
						InputPrice: billingFloat(.1), OutputPrice: billingFloat(.2),
					}},
				}
				if tc.input != nil || tc.output != nil {
					group.ModelPricing = append(group.ModelPricing, ChannelModelPricing{
						Platform: PlatformOpenAI, Models: []string{publicModel}, BillingMode: BillingModeToken,
						InputPrice: tc.input, OutputPrice: tc.output,
					})
				}
				key := &APIKey{ID: 11, Group: group, GroupID: &group.ID, User: user}
				account := &Account{
					ID: 33, Platform: PlatformOpenAI, Type: AccountTypeAPIKey,
					Credentials: map[string]any{"model_mapping": map[string]any{publicModel: upstreamModel}},
				}
				billing := NewBillingService(&config.Config{}, nil)
				billing.frozenPricingFallbackModels = map[string]string{PlatformOpenAI: "gpt-5.4"}
				cache := populateChannelCache([]Channel{{
					ID: 1, Status: StatusActive, GroupIDs: []int64{group.ID}, BillingModelSource: BillingModelSourceRequested,
					ModelPricing: []ChannelModelPricing{{
						Platform: PlatformOpenAI, Models: []string{"gpt-5.4"}, BillingMode: BillingModeToken,
						InputPrice: billingFloat(.3), OutputPrice: billingFloat(.4),
					}},
				}}, map[int64]string{group.ID: PlatformOpenAI})
				channels := &ChannelService{frozenCache: cache, billingService: billing}
				resolver := NewModelPricingResolver(channels, billing)
				at := time.Date(2026, 10, 25, 12, 0, 0, 0, time.UTC)
				ctx := withBillingPricingSnapshot(WithResolvedTargetPlatform(context.Background(), PlatformOpenAI), key, at, billing, channels, nil, publicModel)
				require.NoError(t, ValidateBillingPricingSnapshot(ctx))
				deps := inflightEstimateDeps{billing: billing, resolver: resolver, accountMappedModels: func(context.Context, *APIKey, string) []string {
					t.Fatal("a public SKU with an explicit platform fallback must not switch to account-mapped retail pricing")
					return nil
				}}
				request := InflightEstimateRequest{Model: publicModel, BodyBytes: 4000, MaxTokens: 1000, Kind: InflightEstimateToken}
				amount, priced := deps.estimate(ctx, key, request)
				require.True(t, priced)
				require.InDelta(t, tc.total, amount, 1e-12, "1000 estimated input and 1000 output tokens must use the settlement unit prices")

				// Detached durable settlement must retain the accepted fallback and
				// partial overrides without rereading retail cards for gpt-5.4.
				encoded, err := ExportBillingPricingSnapshot(ctx, upstreamModel)
				require.NoError(t, err)
				ctx, err = RestoreBillingPricingSnapshot(context.Background(), encoded)
				require.NoError(t, err)
				amount, priced = deps.estimate(ctx, key, request)
				require.True(t, priced)
				require.InDelta(t, tc.total, amount, 1e-12)

				if gateway == "generic" {
					svc := newGatewayRecordUsageServiceWithBillingRepoForTest(logs, ledger, &openAIRecordUsageUserRepoStub{}, &openAIRecordUsageSubRepoStub{})
					svc.billingService, svc.channelService, svc.resolver = billing, channels, resolver
					input := &RecordUsageInput{
						Result: &ForwardResult{
							RequestID: "platform-fallback-generic", Model: publicModel, UpstreamModel: upstreamModel,
							Usage: ClaudeUsage{InputTokens: 1000, OutputTokens: 1000},
						},
						APIKey: key, User: user, Account: account, OriginalModel: publicModel,
					}
					require.InDelta(t, tc.total, svc.QuoteUsageCost(ctx, input), 1e-12)
					require.NoError(t, svc.RecordUsage(ctx, input))
					require.Equal(t, publicModel, input.Result.Model)
					require.Equal(t, upstreamModel, input.Result.UpstreamModel)
				} else {
					svc := newOpenAIRecordUsageServiceWithBillingRepoForTest(logs, ledger, &openAIRecordUsageUserRepoStub{}, &openAIRecordUsageSubRepoStub{}, nil)
					svc.billingService, svc.channelService, svc.resolver = billing, channels, resolver
					input := &OpenAIRecordUsageInput{
						Result: &OpenAIForwardResult{
							RequestID: "platform-fallback-openai", Model: publicModel, BillingModel: publicModel, UpstreamModel: upstreamModel,
							Usage: OpenAIUsage{InputTokens: 1000, OutputTokens: 1000},
						},
						APIKey: key, User: user, Account: account, OriginalModel: publicModel,
					}
					require.NoError(t, svc.RecordUsage(ctx, input))
					require.Equal(t, publicModel, input.Result.Model)
					require.Equal(t, publicModel, input.Result.BillingModel)
					require.Equal(t, upstreamModel, input.Result.UpstreamModel)
				}
				require.Equal(t, 1, logs.calls)
				require.Equal(t, 1, ledger.calls)
				require.NotNil(t, logs.lastLog)
				require.NotNil(t, ledger.lastCmd)
				require.InDelta(t, tc.inputCost, logs.lastLog.InputCost, 1e-12)
				require.InDelta(t, tc.outputCost, logs.lastLog.OutputCost, 1e-12)
				require.InDelta(t, tc.total, logs.lastLog.TotalCost, 1e-12)
				require.InDelta(t, tc.total, logs.lastLog.ActualCost, 1e-12)
				require.InDelta(t, tc.total, ledger.lastCmd.BalanceCost, 1e-12)
				require.Equal(t, publicModel, logs.lastLog.Model)
				require.Equal(t, publicModel, logs.lastLog.RequestedModel)
				require.NotNil(t, logs.lastLog.UpstreamModel)
				require.Equal(t, upstreamModel, *logs.lastLog.UpstreamModel)
				require.Equal(t, publicModel, ledger.lastCmd.Model)
				require.Equal(t, 1000, logs.lastLog.InputTokens)
				require.Equal(t, 1000, logs.lastLog.OutputTokens)
				require.True(t, group.ModelAllowlist.Allows(publicModel))
				require.False(t, group.ModelAllowlist.Allows("gpt-5.4"), "pricing fallback must not grant model permission")
				require.True(t, account.IsModelSupported(publicModel))
				require.False(t, account.IsModelSupported("gpt-5.4"))
				require.Equal(t, upstreamModel, account.GetMappedModel(publicModel))
			})
		}
	}
}

func TestPlatformPricingFallbackSettlement_TokenCostWithoutResolverUsesGroupOrResolvedPlatform(t *testing.T) {
	for _, tc := range []struct {
		name     string
		ctx      context.Context
		group    *Group
		expected float64
	}{
		{name: "concrete group", ctx: context.Background(), group: &Group{ID: 77, Platform: PlatformOpenAI, RateMultiplier: 1}, expected: .0175},
		{name: "resolved composite platform", ctx: WithResolvedTargetPlatform(context.Background(), PlatformOpenAI), group: &Group{ID: 77, Platform: PlatformComposite, RateMultiplier: 1}, expected: .0175},
		{name: "resolved platform without group", ctx: WithResolvedTargetPlatform(context.Background(), PlatformOpenAI), expected: .0175},
		{name: "partial group input", ctx: context.Background(), group: &Group{ID: 77, Platform: PlatformOpenAI, RateMultiplier: 1,
			ModelPricing: []ChannelModelPricing{{Platform: PlatformOpenAI, Models: []string{"unknown-public"}, BillingMode: BillingModeToken, InputPrice: billingFloat(2e-6)}}}, expected: .017},
	} {
		t.Run(tc.name, func(t *testing.T) {
			billing := NewBillingService(&config.Config{}, nil)
			billing.frozenPricingFallbackModels = map[string]string{PlatformOpenAI: "gpt-5.4"}
			request := TokenCostRequest{
				Ctx: tc.ctx, Group: tc.group, Model: "unknown-public", RateMultiplier: 1,
				Tokens: UsageTokens{InputTokens: 1000, OutputTokens: 1000},
			}
			require.Nil(t, request.Resolver)
			cost, err := billing.CalculateTokenCostForRequest(request)
			require.NoError(t, err)
			require.NotNil(t, cost)
			require.InDelta(t, tc.expected, cost.TotalCost, 1e-12)
			require.InDelta(t, tc.expected, cost.ActualCost, 1e-12)
			require.Equal(t, "unknown-public", request.Model)
		})
	}
}

func TestPlatformPricingFallbackSettlement_OpenAIUsageTokenCostWithoutGroupOrResolver(t *testing.T) {
	for _, tc := range []struct {
		name string
		ctx  context.Context
		key  *APIKey
	}{
		{name: "explicit concrete context", ctx: WithResolvedTargetPlatform(context.Background(), PlatformOpenAI), key: &APIKey{ID: 11}},
		{name: "ungrouped user defaults to OpenAI", ctx: context.Background(), key: &APIKey{ID: 11, User: &User{ID: 22}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			billing := NewBillingService(&config.Config{}, nil)
			billing.frozenPricingFallbackModels = map[string]string{PlatformOpenAI: "gpt-5.4"}
			svc := &OpenAIGatewayService{billingService: billing}
			require.Nil(t, svc.resolver)
			require.Nil(t, tc.key.Group)
			cost, err := svc.calculateOpenAIRecordUsageTokenCost(
				tc.ctx, tc.key, "unknown-public", 1, time.Date(2026, 10, 28, 0, 0, 0, 0, time.UTC),
				UsageTokens{InputTokens: 1000, OutputTokens: 1000}, "", "", nil, false,
			)
			require.NoError(t, err)
			require.NotNil(t, cost)
			require.InDelta(t, .0025, cost.InputCost, 1e-12)
			require.InDelta(t, .015, cost.OutputCost, 1e-12)
			require.InDelta(t, .0175, cost.TotalCost, 1e-12)
			require.InDelta(t, .0175, cost.ActualCost, 1e-12)
		})
	}
}

func TestPlatformPricingFallbackSettlement_OpenAIUsageLongContextGatePreservesUngroupedFalseAndGroupPolicy(t *testing.T) {
	enabled, disabled := true, false
	for _, model := range []string{"gpt-6.1-terra", "unknown-public"} {
		for _, tc := range []struct {
			name        string
			group       *Group
			gate        *bool
			modelExempt bool
			inputCost   float64
			outputCost  float64
			total       float64
			applied     bool
		}{
			{name: "ungrouped explicit false", gate: &disabled, inputCost: .3, outputCost: .005, total: .305},
			{name: "ungrouped explicit true", gate: &enabled, inputCost: .6, outputCost: .0075, total: .6075, applied: true},
			{name: "ungrouped no gate keeps default", inputCost: .6, outputCost: .0075, total: .6075, applied: true},
			{name: "group enabled survives account false", group: &Group{ID: 77, Platform: PlatformOpenAI, LongContextPricingEnabled: true}, gate: &disabled, inputCost: .6, outputCost: .0075, total: .6075, applied: true},
			{name: "group disabled and account false", group: &Group{ID: 77, Platform: PlatformOpenAI}, gate: &disabled, inputCost: .3, outputCost: .005, total: .305},
			{name: "group disabled without account gate", group: &Group{ID: 77, Platform: PlatformOpenAI}, inputCost: .3, outputCost: .005, total: .305},
			{name: "account true enables disabled group", group: &Group{ID: 77, Platform: PlatformOpenAI}, gate: &enabled, inputCost: .6, outputCost: .0075, total: .6075, applied: true},
			{name: "model exemption still bypasses surcharge", group: &Group{ID: 77, Platform: PlatformOpenAI, LongContextPricingEnabled: true}, gate: &enabled, modelExempt: true, inputCost: .3, outputCost: .005, total: .305},
		} {
			t.Run(model+"/"+tc.name, func(t *testing.T) {
				billing := NewBillingService(&config.Config{}, nil)
				billing.frozenPricingFallbackModels = map[string]string{PlatformOpenAI: "gpt-6.1-terra"}
				svc := &OpenAIGatewayService{billingService: billing}
				key := &APIKey{ID: 11, User: &User{ID: 22}, Group: tc.group}
				// Reviewed Terra rates: $1/M input and $5/M output, then
				// input x2 / output x1.5 above 272k when the gate is enabled.
				cost, err := svc.calculateOpenAIRecordUsageTokenCost(
					context.Background(), key, model, 1, time.Date(2026, 10, 28, 0, 0, 0, 0, time.UTC),
					UsageTokens{InputTokens: 300000, OutputTokens: 1000}, "", "", tc.gate, tc.modelExempt,
				)
				require.NoError(t, err)
				require.NotNil(t, cost)
				require.InDelta(t, tc.inputCost, cost.InputCost, 1e-12)
				require.InDelta(t, tc.outputCost, cost.OutputCost, 1e-12)
				require.InDelta(t, tc.total, cost.TotalCost, 1e-12)
				require.InDelta(t, tc.total, cost.ActualCost, 1e-12)
				require.Equal(t, tc.applied, cost.LongContextBillingApplied)
				require.Nil(t, svc.resolver)
			})
		}
	}
}
