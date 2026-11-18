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

func TestInflightVertexSelectedEndpointMatchesSettlement(t *testing.T) {
	const model = "gemini-3.8-flash"
	for _, tc := range []struct {
		name, location, cacheLocation, wire, mode string
		configured, free, express, search         bool
		want                                      float64
	}{
		{name: "US default token price", location: "us", want: 2.2},
		{name: "EU default token price", location: "eu", want: 2.2},
		{name: "single region", location: "us-central1", want: 2},
		{name: "mapped unsupported model uses global", location: "us", wire: "gemini-2.5-flash", want: 2},
		{name: "explicit global cache overrides US account", location: "us", cacheLocation: "global", want: 2},
		{name: "explicit US cache overrides global account", location: "global", cacheLocation: "us", want: 2.2},
		{name: "express key uses global", location: "us", express: true, want: 2},
		{name: "configured token price", location: "us", configured: true, want: 2},
		{name: "configured free token price", location: "us", configured: true, free: true, want: 0},
		{name: "configured request price", location: "us", configured: true, mode: string(BillingModePerRequest), want: 2},
		{name: "search surcharge is not marked up", location: "us", search: true, want: 3.2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := &config.Config{}
			cfg.Default.RateMultiplier = 1
			pricing := &PricingService{}
			data, err := pricing.parsePricingData([]byte(`{"gemini-3.8-flash":{"input_cost_per_token":0,"output_cost_per_token":2}}`))
			require.NoError(t, err)
			pricing.pricingData = data
			billing := NewBillingService(cfg, pricing)
			group := &Group{ID: 7, Platform: PlatformGemini, Hydrated: true, RateMultiplier: 1}
			if tc.configured {
				output := 2.0
				if tc.free {
					output = 0
				}
				mode := BillingModeToken
				if tc.mode != "" {
					mode = BillingMode(tc.mode)
				}
				group.ModelPricing = []ChannelModelPricing{{Platform: PlatformGemini, Models: []string{model}, BillingMode: mode,
					InputPrice: billingFloat(0), OutputPrice: billingFloat(output), PerRequestPrice: billingFloat(2)}}
			}
			if tc.search {
				group.SearchPricePer1k = billingFloat(1000)
			}
			key := &APIKey{ID: 8, User: &User{ID: 9}, Group: group, GroupID: &group.ID}
			gateway := &GatewayService{cfg: cfg, billingService: billing, resolver: NewModelPricingResolver(nil, billing)}
			account := &Account{ID: 10, Platform: PlatformGemini, Type: AccountTypeServiceAccount,
				Credentials: map[string]any{"project_id": "p1", "location": tc.location}}
			if tc.express {
				account.Credentials = map[string]any{"auth_mode": "apikey", "api_key": "fixture-key", "location": tc.location}
			}
			wire := model
			if tc.wire != "" {
				wire = tc.wire
				account.Credentials["model_mapping"] = map[string]any{model: wire}
			}
			body := []byte(`{"contents":[{"role":"user","parts":[{"text":"hi"}]}]}`)
			if tc.cacheLocation != "" {
				body = []byte(fmt.Sprintf(`{"cachedContent":"projects/p1/locations/%s/cachedContents/cache1"}`, tc.cacheLocation))
			}
			ctx := gateway.WithBillingPricingSnapshot(context.Background(), key, time.Date(2026, 11, 18, 12, 0, 0, 0, time.UTC), model)
			require.NoError(t, ValidateBillingPricingSnapshot(ctx))
			req := (InflightEstimateRequest{Protocol: ContentModerationProtocolGemini, Model: model, MaxTokens: 1}).WithRequestPricingMetadata(body)
			result := &ForwardResult{Model: model, UpstreamModel: wire, Usage: ClaudeUsage{OutputTokens: 1},
				UpstreamPriceMultiplier: VertexUpstreamPriceMultiplier(account, wire, tc.cacheLocation)}
			if tc.search {
				req.SearchCalls, result.SearchCount = 1, 1
			}
			amount, priced := gateway.EstimateInflightReservationForAccount(ctx, key, req, account)
			require.True(t, priced)
			require.InDelta(t, tc.want, amount, 1e-12, "selected endpoint determines the default token charge before sending")
			quote := gateway.QuoteUsageCost(ctx, &RecordUsageInput{APIKey: key, User: key.User, Account: account, Result: result})
			require.InDelta(t, tc.want, quote, 1e-12, "settlement must retain the same independently specified customer charge")
		})
	}
}

// Native handlers retain the pre-channel request model for admission, forward
// the channel result, then apply the selected account's mapping exactly once.
// Each source selects its own price card; the endpoint factor follows the wire
// model independently of that retail selection.
func TestInflightVertexChannelAndAccountMappingRetainBillingSource(t *testing.T) {
	const requested, channelModel, repeated, wire = "gemini-3.8-flash", "gemini-3.7-flash", "gemini-2.5-flash-lite", "gemini-4-argon"
	for _, tc := range []struct {
		source string
		want   float64
	}{
		{source: BillingModelSourceRequested, want: 2.2},
		{source: BillingModelSourceChannelMapped, want: 4.4},
		{source: BillingModelSourceUpstream, want: 17.6},
	} {
		t.Run(tc.source, func(t *testing.T) {
			cfg := &config.Config{}
			cfg.Default.RateMultiplier = 1
			pricing := &PricingService{}
			data, err := pricing.parsePricingData([]byte(`{
				"gemini-3.8-flash":{"input_cost_per_token":0,"output_cost_per_token":2},
				"gemini-3.7-flash":{"input_cost_per_token":0,"output_cost_per_token":4},
				"gemini-2.5-flash-lite":{"input_cost_per_token":0,"output_cost_per_token":8},
				"gemini-4-argon":{"input_cost_per_token":0,"output_cost_per_token":16}
			}`))
			require.NoError(t, err)
			pricing.pricingData = data
			group := &Group{ID: 70, Platform: PlatformGemini, Hydrated: true, RateMultiplier: 1}
			key := &APIKey{ID: 80, User: &User{ID: 90}, Group: group, GroupID: &group.ID}
			cache := populateChannelCache([]Channel{{ID: 71, Status: StatusActive, GroupIDs: []int64{group.ID}, BillingModelSource: tc.source,
				ModelMapping: map[string]map[string]string{PlatformGemini: {requested: channelModel, channelModel: repeated}}}},
				map[int64]string{group.ID: PlatformGemini})
			channels := &ChannelService{frozenCache: cache}
			channels.cache.Store(cache)
			billing := NewBillingService(cfg, pricing)
			gateway := &GatewayService{cfg: cfg, billingService: billing, channelService: channels, resolver: NewModelPricingResolver(channels, billing)}
			account := &Account{ID: 100, Platform: PlatformGemini, Type: AccountTypeServiceAccount,
				Credentials: map[string]any{"project_id": "p1", "location": "us", "model_mapping": map[string]any{channelModel: wire}}}
			ctx := gateway.WithBillingPricingSnapshot(context.Background(), key, time.Date(2026, 11, 18, 12, 0, 0, 0, time.UTC), requested)
			require.NoError(t, ValidateBillingPricingSnapshot(ctx))
			mapping := channels.ResolveChannelMapping(ctx, group.ID, requested)
			require.Equal(t, channelModel, mapping.MappedModel)
			req := (InflightEstimateRequest{Protocol: ContentModerationProtocolGemini, Model: requested, MaxTokens: 1}).WithRequestPricingMetadata(
				[]byte(`{"contents":[{"role":"user","parts":[{"text":"hi"}]}],"generationConfig":{"maxOutputTokens":1}}`))
			amount, priced := gateway.EstimateInflightReservationForAccount(ctx, key, req, account)
			require.True(t, priced)
			require.InDelta(t, tc.want, amount, 1e-12)
			quote := gateway.QuoteUsageCost(ctx, &RecordUsageInput{APIKey: key, User: key.User, Account: account,
				Result:             &ForwardResult{Model: channelModel, UpstreamModel: wire, Usage: ClaudeUsage{OutputTokens: 1}, UpstreamPriceMultiplier: 1.1},
				ChannelUsageFields: mapping.ToUsageFields(requested, wire)})
			require.InDelta(t, tc.want, quote, 1e-12)
		})
	}
}
