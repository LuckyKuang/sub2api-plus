//go:build unit

package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/config"
	"github.com/stretchr/testify/require"
)

func platformFallbackTestBilling(catalog *PricingService) *BillingService {
	billing := NewBillingService(&config.Config{}, catalog)
	// Pin reviewed promotional rates and avoid a dependency on the wall clock.
	billing.frozenPricingAt = time.Date(2026, 10, 25, 12, 0, 0, 0, time.UTC)
	return billing
}

func requirePlatformFallbackRates(t *testing.T, price *ModelPricing, input, output float64) {
	t.Helper()
	require.NotNil(t, price)
	require.InDelta(t, input, price.InputPricePerToken, 1e-15)
	require.InDelta(t, output, price.OutputPricePerToken, 1e-15)
}

func TestPlatformPricingFallback_DefaultsHaveExactSystemPricesForAllConcretePlatforms(t *testing.T) {
	// Expected SKUs and rates come from the reviewed platform defaults and their
	// published cards. Do not calculate these expectations with the resolver.
	cases := []struct {
		platform, model string
		input, output   float64
	}{
		{"anthropic", "claude-opus-5-5", 4e-6, 20e-6},
		{"openai", "gpt-6.1-sol", 2e-6, 10e-6},
		{"gemini", "gemini-3.8-flash", 0.75e-6, 3.75e-6},
		{"antigravity", "gemini-3.8-flash", 0.75e-6, 3.75e-6},
		{"grok", "grok-4.6", 2e-6, 6e-6},
		{"kimi", "kimi-k3", 3e-6, 15e-6},
		{"zhipu", "glm-5.3", 1.4e-6, 4.4e-6},
		{"deepseek", "deepseek-v4.1-flash", 0.15e-6, 0.60e-6},
		{"minimax", "minimax-m3", 0.60e-6, 2.40e-6},
		{"stepfun", "step-5-preview", 1e-6, 2.70e-6},
		{"opencode_go", "glm-5.3", 1.4e-6, 4.4e-6},
		{"typesafe", "jev-latest", 0.042e-6, 0},
		{"command_code", "deepseek-v4.1-flash", 0.15e-6, 0.60e-6},
		{"cline", "deepseek-v4.1-flash", 0.15e-6, 0.60e-6},
	}
	defaults := DefaultPricingFallbackModels()
	require.Len(t, defaults, 14)
	for _, tc := range cases {
		t.Run(tc.platform, func(t *testing.T) {
			billing := platformFallbackTestBilling(nil)
			require.Equal(t, tc.model, defaults[tc.platform])
			exact, err := billing.GetModelPricingForPlatform(context.Background(), tc.platform, tc.model)
			require.NoError(t, err)
			requirePlatformFallbackRates(t, exact, tc.input, tc.output)
			require.Empty(t, exact.FallbackModel, "the configured fallback SKU must itself have an exact system price")
			require.Empty(t, exact.FallbackPlatform)

			fallback, err := billing.GetModelPricingForPlatform(context.Background(), tc.platform, "unpublished-request-sku")
			require.NoError(t, err)
			requirePlatformFallbackRates(t, fallback, tc.input, tc.output)
			require.Equal(t, tc.model, fallback.FallbackModel)
			require.Equal(t, tc.platform, fallback.FallbackPlatform)
		})
	}
}

func TestPlatformPricingFallback_SystemSameSKUPrecedesPlatformFallback(t *testing.T) {
	catalog := newStubPricingServiceFromJSON(t, `{
		"global-priced-sku": {"litellm_provider":"openai", "input_cost_per_token":0.000002, "output_cost_per_token":0.000006},
		"global-explicit-free": {"litellm_provider":"openai", "input_cost_per_token":0, "output_cost_per_token":0},
		"gpt-6.1-terra-unpublished-sku": {"litellm_provider":"openai", "input_cost_per_token":0.000003, "output_cost_per_token":0.000007},
		"gpt-5.4-mini": {"litellm_provider":"openai", "input_cost_per_token":0.000001, "output_cost_per_token":0.000002}
	}`)
	for _, tc := range []struct {
		name, model   string
		catalog       *PricingService
		input, output float64
	}{
		{"global exact", "global-priced-sku", catalog, 2e-6, 6e-6},
		{"global explicit zero", "global-explicit-free", catalog, 0, 0},
		{"explicit global unknown Terra SKU", "gpt-6.1-terra-unpublished-sku", catalog, 3e-6, 7e-6},
		{"global overrides compiled exact", "gpt-5.4-mini", catalog, 1e-6, 2e-6},
		{"compiled exact", "gpt-5.4-mini", nil, 0.75e-6, 4.5e-6},
		{"compiled canonical Terra exact", "gpt-6.1-terra", nil, 1e-6, 5e-6},
		{"registered same SKU alias", "claude-sonnet-5-thinking", nil, 2e-6, 10e-6},
	} {
		t.Run(tc.name, func(t *testing.T) {
			billing := platformFallbackTestBilling(tc.catalog)
			billing.frozenPricingFallbackModels = map[string]string{"openai": "grok-4.6"}
			price, err := billing.GetModelPricingForPlatform(context.Background(), "openai", tc.model)
			require.NoError(t, err)
			requirePlatformFallbackRates(t, price, tc.input, tc.output)
			require.Empty(t, price.FallbackModel)
			require.Empty(t, price.FallbackPlatform)
		})
	}
}

func TestPlatformPricingFallback_UnknownTerraNeedsTheExplicitConcretePlatformKey(t *testing.T) {
	billing := platformFallbackTestBilling(nil)
	billing.frozenPricingFallbackModels = map[string]string{"openai": "gpt-5.4", "grok": "grok-4.6"}
	for _, tc := range []struct {
		platform, fallback string
		input, output      float64
	}{
		{"openai", "gpt-5.4", 2.5e-6, 15e-6},
		{"grok", "grok-4.6", 2e-6, 6e-6},
	} {
		t.Run(tc.platform, func(t *testing.T) {
			price, err := billing.GetModelPricingForPlatform(context.Background(), tc.platform, "gpt-6.1-terra-unpublished-sku")
			require.NoError(t, err)
			requirePlatformFallbackRates(t, price, tc.input, tc.output)
			require.Equal(t, tc.fallback, price.FallbackModel)
			require.Equal(t, tc.platform, price.FallbackPlatform)
		})
	}
	for _, platform := range []string{"", "composite", "unknown-platform", "kimi"} {
		t.Run("no configured concrete key/"+platform, func(t *testing.T) {
			price, err := billing.GetModelPricingForPlatform(context.Background(), platform, "gpt-6.1-terra-unpublished-sku")
			require.ErrorIs(t, err, ErrModelPricingUnavailable)
			require.Nil(t, price)
		})
	}
	billing.frozenPricingFallbackModels["openai"] = ""
	price, err := billing.GetModelPricingForPlatform(context.Background(), "openai", "gpt-6.1-terra-unpublished-sku")
	require.ErrorIs(t, err, ErrModelPricingUnavailable)
	require.Nil(t, price, "an explicitly disabled platform must not borrow Grok's configured fallback")
}

func TestPlatformPricingFallback_EmptyRequestModelCannotSelectAFallback(t *testing.T) {
	billing := platformFallbackTestBilling(nil)
	for _, model := range []string{"", "   "} {
		price, err := billing.GetModelPricingForPlatform(context.Background(), PlatformOpenAI, model)
		require.ErrorIs(t, err, ErrModelPricingUnavailable)
		require.Nil(t, price)
	}
}

func TestPlatformPricingFallback_UsesExactFallbackSKUWithoutGuessingOrRecursing(t *testing.T) {
	catalog := newStubPricingServiceFromJSON(t, `{
		"image-only-fallback": {"litellm_provider":"openai", "mode":"image_generation", "output_cost_per_image":0.02}
	}`)
	for _, fallback := range []string{
		"unpriced-fallback", "claude-unpublished-future", "gpt-6.1-terra-unpublished-sku", "image-only-fallback",
	} {
		t.Run(fallback, func(t *testing.T) {
			billing := platformFallbackTestBilling(catalog)
			billing.frozenPricingFallbackModels = map[string]string{"openai": fallback, "anthropic": "claude-sonnet-5"}
			price, err := billing.GetModelPricingForPlatform(context.Background(), "openai", "unpublished-request-sku")
			require.ErrorIs(t, err, ErrModelPricingUnavailable)
			require.Nil(t, price)
		})
	}
	t.Run("original family name does not bypass the platform chain", func(t *testing.T) {
		billing := platformFallbackTestBilling(nil)
		billing.frozenPricingFallbackModels = map[string]string{"openai": "gpt-5.4"}
		price, err := billing.GetModelPricingForPlatform(context.Background(), "openai", "claude-unpublished-future")
		require.NoError(t, err)
		requirePlatformFallbackRates(t, price, 2.5e-6, 15e-6)
		require.Equal(t, "gpt-5.4", price.FallbackModel)
	})
	t.Run("self fallback is not a price", func(t *testing.T) {
		billing := platformFallbackTestBilling(nil)
		billing.frozenPricingFallbackModels = map[string]string{"openai": "gpt-6.1-terra-unpublished-sku"}
		price, err := billing.GetModelPricingForPlatform(context.Background(), "openai", "gpt-6.1-terra-unpublished-sku")
		require.ErrorIs(t, err, ErrModelPricingUnavailable)
		require.Nil(t, price)
	})
}

func TestPlatformPricingFallback_ResolverUsesGroupOrResolvedConcretePlatformWithoutChangingModels(t *testing.T) {
	billing := platformFallbackTestBilling(nil)
	billing.frozenPricingFallbackModels = map[string]string{
		"openai": "gpt-5.4", "grok": "grok-4.6", "command_code": "claude-sonnet-5",
	}
	resolver := NewModelPricingResolver(nil, billing)
	for _, tc := range []struct {
		name, groupPlatform, resolvedPlatform, fallback string
		input, output                                   float64
	}{
		{"concrete group", "openai", "", "gpt-5.4", 2.5e-6, 15e-6},
		{"composite OpenAI route", "composite", "openai", "gpt-5.4", 2.5e-6, 15e-6},
		{"composite Grok route", "composite", "grok", "grok-4.6", 2e-6, 6e-6},
		{"aggregate supplier can name another vendor SKU", "command_code", "", "claude-sonnet-5", 2e-6, 10e-6},
	} {
		t.Run(tc.name, func(t *testing.T) {
			group := &Group{ID: 77, Platform: tc.groupPlatform, RateMultiplier: 1}
			ctx := context.Background()
			if tc.resolvedPlatform != "" {
				ctx = WithCompositeRouteDecision(ctx, CompositeRouteDecision{
					Matched: true, TargetPlatform: tc.resolvedPlatform,
					PublicModel: "public-terra-alias", UpstreamModel: "gpt-6.1-terra-unpublished-sku",
				})
			}
			input := PricingInput{Model: "gpt-6.1-terra-unpublished-sku", Group: group}
			resolved := resolver.Resolve(ctx, input)
			require.Equal(t, PricingSourcePlatformFallback, resolved.Source)
			requirePlatformFallbackRates(t, resolved.BasePricing, tc.input, tc.output)
			require.Equal(t, tc.fallback, resolved.BasePricing.FallbackModel)
			require.Equal(t, "gpt-6.1-terra-unpublished-sku", input.Model)
			require.Empty(t, group.ModelPricing, "using a default must not materialize a retail model override")
			if tc.resolvedPlatform != "" {
				publicModel, ok := RequestedPublicModelFromContext(ctx)
				require.True(t, ok)
				require.Equal(t, "public-terra-alias", publicModel)
				upstreamModel, ok := ResolvedUpstreamModelFromContext(ctx)
				require.True(t, ok)
				require.Equal(t, "gpt-6.1-terra-unpublished-sku", upstreamModel)
			}
		})
	}
}

func TestPlatformPricingFallback_ConfiguredGroupChannelWildcardAndZeroPricesWin(t *testing.T) {
	p := func(value float64) *float64 { return &value }
	for _, tc := range []struct {
		name, pattern, source    string
		groupInput, channelInput *float64
		wantInput                float64
	}{
		{"channel exact", "gpt-6.1-terra-unpublished-sku", PricingSourceChannel, nil, p(1e-6), 1e-6},
		{"channel zero", "gpt-6.1-terra-unpublished-sku", PricingSourceChannel, nil, p(0), 0},
		{"channel wildcard", "gpt-6.1-*", PricingSourceChannel, nil, p(1e-6), 1e-6},
		{"group exact beats channel", "gpt-6.1-terra-unpublished-sku", PricingSourceGroup, p(2e-6), p(1e-6), 2e-6},
		{"group zero beats channel", "gpt-6.1-terra-unpublished-sku", PricingSourceGroup, p(0), p(1e-6), 0},
		{"group wildcard", "gpt-6.1-*", PricingSourceGroup, p(2e-6), nil, 2e-6},
	} {
		t.Run(tc.name, func(t *testing.T) {
			billing := platformFallbackTestBilling(nil)
			billing.frozenPricingFallbackModels = map[string]string{"openai": "gpt-5.4"}
			group := &Group{ID: 77, Platform: PlatformOpenAI, RateMultiplier: 1}
			var channels *ChannelService
			if tc.channelInput != nil {
				channels = newChannelServiceWithPricings(group.ID, []ChannelModelPricing{{
					Platform: PlatformOpenAI, Models: []string{tc.pattern}, BillingMode: BillingModeToken,
					InputPrice: tc.channelInput,
				}})
			}
			if tc.groupInput != nil {
				group.ModelPricing = []ChannelModelPricing{{
					Platform: PlatformOpenAI, Models: []string{tc.pattern}, BillingMode: BillingModeToken,
					InputPrice: tc.groupInput,
				}}
			}
			resolver := NewModelPricingResolver(channels, billing)
			resolved := resolver.Resolve(context.Background(), PricingInput{Group: group, GroupID: &group.ID, Model: "gpt-6.1-terra-unpublished-sku"})
			require.Equal(t, tc.source, resolved.Source)
			requirePlatformFallbackRates(t, resolved.BasePricing, tc.wantInput, 15e-6)
		})
	}
}

func TestPlatformPricingFallback_DoesNotReadRetailPricesForTheFallbackSKU(t *testing.T) {
	billing := platformFallbackTestBilling(nil)
	billing.frozenPricingFallbackModels = map[string]string{"openai": "gpt-5.4"}
	zero := 0.0
	group := &Group{ID: 77, Platform: PlatformOpenAI, RateMultiplier: 1, ModelPricing: []ChannelModelPricing{{
		Platform: PlatformOpenAI, Models: []string{"gpt-5.4"}, BillingMode: BillingModeToken,
		InputPrice: &zero, OutputPrice: &zero,
	}}}
	channels := newChannelServiceWithPricings(group.ID, group.ModelPricing)
	resolver := NewModelPricingResolver(channels, billing)
	resolved := resolver.Resolve(context.Background(), PricingInput{Group: group, GroupID: &group.ID, Model: "gpt-6.1-terra-unpublished-sku"})
	require.Equal(t, PricingSourcePlatformFallback, resolved.Source)
	requirePlatformFallbackRates(t, resolved.BasePricing, 2.5e-6, 15e-6)
	exact, err := billing.GetModelPricingForPlatform(context.Background(), "openai", "gpt-5.4")
	require.NoError(t, err)
	require.Empty(t, exact.FallbackModel, "fallback attribution must not mutate the shared system card")
	require.Empty(t, exact.FallbackPlatform)
}

func TestPlatformPricingFallback_SnapshotPinsConfigurationAndDurablePriceEvidence(t *testing.T) {
	configured := map[string]string{"openai": "gpt-5.4"}
	billing := platformFallbackTestBilling(nil)
	reads := 0
	billing.pricingFallbackModels = func(context.Context) (map[string]string, error) {
		reads++
		return configured, nil
	}
	group := &Group{ID: 77, Platform: PlatformOpenAI, RateMultiplier: 1}
	key := &APIKey{Group: group, GroupID: &group.ID}
	oldCtx := withBillingPricingSnapshot(context.Background(), key, billing.frozenPricingAt, billing, nil, nil, "gpt-6.1-terra-unpublished-sku")
	require.NoError(t, ValidateBillingPricingSnapshot(oldCtx))
	require.Equal(t, 1, reads)

	configured["openai"] = "gpt-5.4-mini"
	oldPrice, err := billing.GetModelPricingForPlatform(oldCtx, "openai", "gpt-6.1-terra-unpublished-sku")
	require.NoError(t, err)
	requirePlatformFallbackRates(t, oldPrice, 2.5e-6, 15e-6)
	require.Equal(t, "gpt-5.4", oldPrice.FallbackModel)
	require.Equal(t, 1, reads, "an accepted request must not reread live fallback settings")

	newCtx := withBillingPricingSnapshot(context.Background(), key, billing.frozenPricingAt, billing, nil, nil, "gpt-6.1-terra-unpublished-sku")
	newPrice, err := billing.GetModelPricingForPlatform(newCtx, "openai", "gpt-6.1-terra-unpublished-sku")
	require.NoError(t, err)
	requirePlatformFallbackRates(t, newPrice, 0.75e-6, 4.5e-6)
	require.Equal(t, "gpt-5.4-mini", newPrice.FallbackModel)
	require.Equal(t, 2, reads)

	encoded, err := ExportBillingPricingSnapshot(oldCtx, "gpt-6.1-terra-unpublished-sku")
	require.NoError(t, err)
	require.Contains(t, string(encoded), `"fallback_model":"gpt-5.4"`)
	require.Contains(t, string(encoded), `"fallback_platform":"openai"`)
	billing.pricingFallbackModels = func(context.Context) (map[string]string, error) {
		return nil, errors.New("live settings are unavailable after process restart")
	}
	restoredCtx, err := RestoreBillingPricingSnapshot(context.Background(), encoded)
	require.NoError(t, err)
	restoredPrice, err := billing.GetModelPricingForPlatform(restoredCtx, "openai", "gpt-6.1-terra-unpublished-sku")
	require.NoError(t, err)
	requirePlatformFallbackRates(t, restoredPrice, 2.5e-6, 15e-6)
	require.Equal(t, "gpt-5.4", restoredPrice.FallbackModel)
	require.Equal(t, "openai", restoredPrice.FallbackPlatform)

	snapshot := BillingPricingSnapshotFromContext(restoredCtx)
	cost, err := snapshot.billing.CalculateTokenCostForRequest(TokenCostRequest{
		Ctx: restoredCtx, Group: snapshot.group, Model: "gpt-6.1-terra-unpublished-sku",
		Tokens: UsageTokens{InputTokens: 1000, OutputTokens: 1000}, RateMultiplier: 1, Resolver: snapshot.resolver,
	})
	require.NoError(t, err)
	require.InDelta(t, 0.0175, cost.TotalCost, 1e-12)
	require.InDelta(t, 0.0175, cost.ActualCost, 1e-12)
	missing, err := billing.GetModelPricingForPlatform(restoredCtx, "openai", "not-captured-in-task")
	require.ErrorIs(t, err, ErrModelPricingUnavailable)
	require.Nil(t, missing, "durable missing evidence cannot be repaired by today's platform fallback")
}

func TestPlatformPricingFallback_SnapshotPinsTheFallbackCatalogGeneration(t *testing.T) {
	catalog := newStubPricingServiceFromJSON(t, `{
		"configured-fallback-sku":{"litellm_provider":"openai","input_cost_per_token":0.000001,"output_cost_per_token":0.000002}
	}`)
	billing := platformFallbackTestBilling(catalog)
	billing.frozenPricingFallbackModels = map[string]string{"openai": "configured-fallback-sku"}
	group := &Group{ID: 77, Platform: PlatformOpenAI, RateMultiplier: 1}
	key := &APIKey{Group: group, GroupID: &group.ID}
	oldCtx := withBillingPricingSnapshot(context.Background(), key, billing.frozenPricingAt, billing, nil, nil, "gpt-6.1-terra-unpublished-sku")
	newData, err := catalog.parsePricingData([]byte(`{
		"configured-fallback-sku":{"litellm_provider":"openai","input_cost_per_token":0.000003,"output_cost_per_token":0.000006}
	}`))
	require.NoError(t, err)
	catalog.mu.Lock()
	catalog.pricingData = newData
	catalog.mu.Unlock()
	oldPrice, err := billing.GetModelPricingForPlatform(oldCtx, "openai", "gpt-6.1-terra-unpublished-sku")
	require.NoError(t, err)
	requirePlatformFallbackRates(t, oldPrice, 1e-6, 2e-6)
	newCtx := withBillingPricingSnapshot(context.Background(), key, billing.frozenPricingAt, billing, nil, nil, "gpt-6.1-terra-unpublished-sku")
	newPrice, err := billing.GetModelPricingForPlatform(newCtx, "openai", "gpt-6.1-terra-unpublished-sku")
	require.NoError(t, err)
	requirePlatformFallbackRates(t, newPrice, 3e-6, 6e-6)
}

func TestPlatformPricingFallback_SettingsFailureCannotBecomeAnInventedFreeCard(t *testing.T) {
	billing := platformFallbackTestBilling(nil)
	billing.pricingFallbackModels = func(context.Context) (map[string]string, error) {
		return nil, errors.New("settings storage unavailable")
	}
	price, err := billing.GetModelPricingForPlatform(context.Background(), "openai", "gpt-6.1-terra-unpublished-sku")
	require.ErrorIs(t, err, ErrModelPricingUnavailable)
	require.Nil(t, price)
	group := &Group{ID: 77, Platform: PlatformOpenAI, RateMultiplier: 1}
	ctx := withBillingPricingSnapshot(context.Background(), &APIKey{Group: group}, billing.frozenPricingAt, billing, nil, nil, "gpt-6.1-terra-unpublished-sku")
	require.Error(t, ValidateBillingPricingSnapshot(ctx))
	_, err = ExportBillingPricingSnapshot(ctx, "gpt-6.1-terra-unpublished-sku")
	require.Error(t, err)
}
