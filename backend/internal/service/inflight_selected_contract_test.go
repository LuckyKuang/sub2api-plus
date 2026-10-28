//go:build unit

package service

import (
	"context"
	"testing"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/config"
	"github.com/stretchr/testify/require"
)

func TestInflightSelectedAccountPreservesChannelSourceContract(t *testing.T) {
	for _, tc := range []struct {
		name, source string
		mapped       bool
		openAI       bool
		want         float64
	}{
		{"openai public card without channel mapping", BillingModelSourceChannelMapped, false, true, .003},
		{"generic default without channel mapping", BillingModelSourceChannelMapped, false, false, .003},
		{"openai mapped channel card", BillingModelSourceChannelMapped, true, true, .007},
		{"generic mapped channel card", BillingModelSourceChannelMapped, true, false, .007},
		{"openai requested card", BillingModelSourceRequested, true, true, .003},
		{"generic requested card", BillingModelSourceRequested, true, false, .003},
		{"openai upstream card", BillingModelSourceUpstream, true, true, .012},
		{"generic upstream card", BillingModelSourceUpstream, true, false, .012},
	} {
		t.Run(tc.name, func(t *testing.T) {
			platform := PlatformAnthropic
			if tc.openAI {
				platform = PlatformOpenAI
			}
			group := &Group{ID: 77, Platform: platform, RateMultiplier: 1, Hydrated: true}
			key := &APIKey{ID: 1, User: &User{ID: 2}, GroupID: &group.ID, Group: group}
			channel := Channel{ID: 7, Status: StatusActive, GroupIDs: []int64{group.ID}, BillingModelSource: tc.source,
				ModelPricing: []ChannelModelPricing{
					{Platform: platform, Models: []string{"retail"}, BillingMode: BillingModeToken, InputPrice: billingFloat(1e-6), OutputPrice: billingFloat(2e-6)},
					{Platform: platform, Models: []string{"channel-model"}, BillingMode: BillingModeToken, InputPrice: billingFloat(3e-6), OutputPrice: billingFloat(4e-6)},
					{Platform: platform, Models: []string{"wire-model"}, BillingMode: BillingModeToken, InputPrice: billingFloat(2e-6), OutputPrice: billingFloat(10e-6)},
				}}
			if tc.mapped {
				channel.ModelMapping = map[string]map[string]string{platform: {"retail": "channel-model"}}
			}
			cache := populateChannelCache([]Channel{channel}, map[int64]string{group.ID: platform})
			channels := &ChannelService{frozenCache: cache}
			channels.cache.Store(cache)
			cfg := &config.Config{}
			cfg.Default.RateMultiplier = 1
			billing := NewBillingService(cfg, nil)
			resolver := NewModelPricingResolver(channels, billing)
			generic := &GatewayService{cfg: cfg, billingService: billing, resolver: resolver, channelService: channels}
			openai := &OpenAIGatewayService{cfg: cfg, billingService: billing, resolver: resolver, channelService: channels}
			account := &Account{ID: 3, Platform: platform, Type: AccountTypeAPIKey, Credentials: map[string]any{
				"model_mapping": map[string]any{"retail": "wire-model", "channel-model": "wire-model"},
			}}
			ctx := generic.WithBillingPricingSnapshot(WithResolvedTargetPlatform(context.Background(), platform), key, time.Now(), "retail")
			req := InflightEstimateRequest{Model: "retail", BodyBytes: 4000, MaxTokens: 1000}
			var amount float64
			var priced bool
			if tc.openAI {
				amount, priced = openai.EstimateInflightReservationForAccount(ctx, key, req, account)
			} else {
				amount, priced = generic.EstimateInflightReservationForAccount(ctx, key, req, account)
				// The generic gateway intentionally retains its existing channel
				// source contract even without a channel mapping. Reconciliation
				// must not silently turn that into upstream-model billing.
				mapping := channels.ResolveChannelMapping(ctx, group.ID, "retail")
				result := &ForwardResult{Model: "retail", UpstreamModel: "wire-model", Usage: ClaudeUsage{InputTokens: 1000, OutputTokens: 1000}}
				quote := generic.QuoteUsageCost(ctx, &RecordUsageInput{APIKey: key, User: key.User, Account: account, Result: result,
					ChannelUsageFields: mapping.ToUsageFields("retail", "wire-model")})
				require.InDelta(t, tc.want, quote, 1e-12)
			}
			require.True(t, priced)
			require.InDelta(t, tc.want, amount, 1e-12)
		})
	}
}

func TestInflightSelectedAccountUsesActualOpenAIMappingMode(t *testing.T) {
	for _, tc := range []struct {
		name                          string
		passthrough, compact, rawChat bool
		ws                            bool
		wsMode                        string
		want                          float64
	}{
		{"ordinary mapping", false, false, false, false, "", .012},
		{"compact mapping", false, true, false, false, "", .0014},
		{"passthrough ignores ordinary mapping", true, false, false, false, "", .0006},
		{"passthrough compact exception", true, true, false, false, "", .0014},
		{"raw chat precedes passthrough and compact", true, true, true, false, "", .012},
		{"WS passthrough keeps the channel model", false, false, false, true, OpenAIWSIngressModePassthrough, .0006},
		{"WS pool maps the account model", false, false, false, true, OpenAIWSIngressModeCtxPool, .012},
		{"HTTP ignores WS mode", false, false, false, false, OpenAIWSIngressModePassthrough, .012},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := rawChatCompletionsTestConfig()
			cfg.Default.RateMultiplier = 1
			cfg.Gateway.OpenAIWS.ModeRouterV2Enabled = true
			cfg.Gateway.OpenAIWS.Enabled = true
			cfg.Gateway.OpenAIWS.APIKeyEnabled = true
			group := &Group{ID: 77, Platform: PlatformOpenAI, RateMultiplier: 1, Hydrated: true}
			key := &APIKey{User: &User{ID: 1}, Group: group, GroupID: &group.ID}
			channels := newChannelServiceWithPricings(group.ID, []ChannelModelPricing{{Platform: PlatformOpenAI,
				Models: []string{"gpt-6-sol"}, BillingMode: BillingModeToken, InputPrice: billingFloat(2e-6), OutputPrice: billingFloat(10e-6)}})
			billing := NewBillingService(cfg, nil)
			gateway := &OpenAIGatewayService{cfg: cfg, billingService: billing, channelService: channels, resolver: NewModelPricingResolver(channels, billing)}
			account := structuredInputRetryTestAccount()
			account.Credentials["model_mapping"] = map[string]any{"gpt-6-luna": "gpt-6-sol"}
			account.Credentials["compact_model_mapping"] = map[string]any{"gpt-6-luna": "gpt-5.6-luna"}
			account.Extra["openai_passthrough"] = tc.passthrough
			if tc.wsMode != "" {
				account.Extra["openai_apikey_responses_websockets_v2_mode"] = tc.wsMode
			}
			if tc.rawChat {
				account.Extra["openai_responses_supported"] = false
			}
			ctx := gateway.WithBillingPricingSnapshot(context.Background(), key, time.Now(), "gpt-6-luna")
			if tc.ws {
				ctx = gateway.WithResponsesWebSocketInflightTransport(ctx, account, []byte(`{"type":"response.create","model":"gpt-6-luna"}`))
			}
			amount, priced := gateway.EstimateInflightReservationForAccount(ctx, key, InflightEstimateRequest{
				Model: "gpt-6-luna", BodyBytes: 4000, MaxTokens: 1000, RequireCompact: tc.compact, ResponsesWebSocket: tc.ws}, account)
			require.True(t, priced)
			require.InDelta(t, tc.want, amount, 1e-12)
		})
	}
}

// An explicit supplier-model source cannot silently charge a cheaper known
// public model, even in a composite group or without a custom supplier card.
func TestInflightSelectedUpstreamCatalogMatchesGenericSettlement(t *testing.T) {
	for _, composite := range []bool{false, true} {
		for _, mapped := range []bool{false, true} {
			name := "ordinary"
			if composite {
				name = "composite"
			}
			if mapped {
				name += "/account-mapped"
			} else {
				name += "/upstream-empty"
			}
			t.Run(name, func(t *testing.T) {
				group := &Group{ID: 77, Platform: PlatformAnthropic, Hydrated: true, RateMultiplier: 1}
				if composite {
					group.Platform = PlatformComposite
				}
				key := &APIKey{User: &User{ID: 1}, Group: group, GroupID: &group.ID}
				cache := populateChannelCache([]Channel{{ID: 7, GroupIDs: []int64{group.ID}, Status: StatusActive,
					BillingModelSource: BillingModelSourceUpstream}}, map[int64]string{group.ID: PlatformAnthropic})
				channels := &ChannelService{frozenCache: cache}
				channels.cache.Store(cache)
				cfg := &config.Config{}
				cfg.Default.RateMultiplier = 1
				billing := NewBillingService(cfg, nil)
				gateway := &GatewayService{cfg: cfg, billingService: billing, channelService: channels, resolver: NewModelPricingResolver(channels, billing)}
				account := &Account{ID: 3, Platform: PlatformAnthropic, Type: AccountTypeAPIKey, Credentials: map[string]any{}}
				result := &ForwardResult{Model: "claude-3-haiku", Usage: ClaudeUsage{InputTokens: 1000, OutputTokens: 1000}}
				want := .0015 // Haiku: $0.25/M input + $1.25/M output.
				if mapped {
					account.Credentials["model_mapping"] = map[string]any{"claude-3-haiku": "claude-sonnet-4"}
					result.UpstreamModel = "claude-sonnet-4"
					want = .018 // Sonnet: $3/M input + $15/M output.
				}
				ctx := gateway.WithBillingPricingSnapshot(WithResolvedTargetPlatform(context.Background(), PlatformAnthropic), key, time.Now(), "claude-3-haiku")
				mapping := channels.ResolveChannelMapping(ctx, group.ID, "claude-3-haiku")
				amount, priced := gateway.EstimateInflightReservationForAccount(ctx, key, InflightEstimateRequest{Model: "claude-3-haiku", BodyBytes: 4000, MaxTokens: 1000}, account)
				require.True(t, priced)
				require.InDelta(t, want, amount, 1e-12)
				quote := gateway.QuoteUsageCost(ctx, &RecordUsageInput{APIKey: key, User: key.User, Account: account, Result: result,
					ChannelUsageFields: mapping.ToUsageFields("claude-3-haiku", result.UpstreamModel)})
				require.InDelta(t, want, quote, 1e-12, "the explicit supplier source must survive composite fallback")
				// Version-1 durable evidence preserves its accepted candidate
				// order rather than adopting a new settlement algorithm.
				legacy := *BillingPricingSnapshotFromContext(ctx)
				legacy.legacyPriceSelection = true
				legacyCtx := pricingSnapshotContext(ctx, &legacy)
				legacyQuote := gateway.QuoteUsageCost(legacyCtx, &RecordUsageInput{APIKey: key, User: key.User, Account: account, Result: result,
					ChannelUsageFields: mapping.ToUsageFields("claude-3-haiku", result.UpstreamModel)})
				require.InDelta(t, .0015, legacyQuote, 1e-12, "accepted legacy tasks cannot be repriced by the source correction")
			})
		}
	}
}

func TestInflightSelectedGenericWithoutChannelKeepsRequestedPrice(t *testing.T) {
	cfg := &config.Config{}
	cfg.Default.RateMultiplier = 1
	group := &Group{ID: 77, Platform: PlatformAnthropic, Hydrated: true, RateMultiplier: 1}
	key := &APIKey{User: &User{ID: 1}, Group: group, GroupID: &group.ID}
	billing := NewBillingService(cfg, nil)
	gateway := &GatewayService{cfg: cfg, billingService: billing, resolver: NewModelPricingResolver(nil, billing)}
	account := &Account{ID: 3, Platform: PlatformAnthropic, Type: AccountTypeAPIKey, Credentials: map[string]any{
		"model_mapping": map[string]any{"claude-3-haiku": "claude-sonnet-4"},
	}}
	ctx := gateway.WithBillingPricingSnapshot(context.Background(), key, time.Now(), "claude-3-haiku")
	amount, priced := gateway.EstimateInflightReservationForAccount(ctx, key, InflightEstimateRequest{
		Model: "claude-3-haiku", BodyBytes: 4000, MaxTokens: 1000}, account)
	require.True(t, priced)
	require.InDelta(t, .0015, amount, 1e-12, "no channel does not opt in to supplier-model billing")
	quote := gateway.QuoteUsageCost(ctx, &RecordUsageInput{APIKey: key, User: key.User, Account: account,
		Result:             &ForwardResult{Model: "claude-3-haiku", UpstreamModel: "claude-sonnet-4", Usage: ClaudeUsage{InputTokens: 1000, OutputTokens: 1000}},
		ChannelUsageFields: ChannelUsageFields{OriginalModel: "claude-3-haiku", ChannelMappedModel: "claude-3-haiku"}})
	require.InDelta(t, .0015, quote, 1e-12)
}
