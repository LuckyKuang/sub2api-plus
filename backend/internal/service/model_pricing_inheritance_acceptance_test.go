//go:build unit

package service

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/config"
	"github.com/LuckyKuang/sub2api-plus/internal/domain"
	"github.com/stretchr/testify/require"
)

func TestPricingEmptyGroupCardInheritsLowerChannelModeAndPrice(t *testing.T) {
	for _, platform := range domain.ConcretePlatformIDs() {
		t.Run(platform, func(t *testing.T) {
			group := &Group{ID: 77, Platform: PlatformComposite, RateMultiplier: 1,
				ModelPricing: []ChannelModelPricing{{Platform: platform, Models: []string{"model001"}, BillingMode: BillingModeToken}}}
			cache := populateChannelCache([]Channel{{ID: 1, Status: StatusActive, GroupIDs: []int64{group.ID}, ModelPricing: []ChannelModelPricing{{Platform: platform, Models: []string{"model001"}, BillingMode: BillingModePerRequest, PerRequestPrice: billingFloat(2)}}}}, map[int64]string{group.ID: PlatformComposite})
			channel := &ChannelService{frozenCache: cache}
			channel.cache.Store(cache)
			billing := NewBillingService(&config.Config{}, nil)
			key := &APIKey{Group: group, GroupID: &group.ID, User: &User{ID: 1}}
			ctx := withBillingPricingSnapshot(WithResolvedTargetPlatform(context.Background(), platform), key, time.Now(), billing, channel, nil, "model001")
			resolver := NewModelPricingResolver(channel, billing)
			resolved := resolver.Resolve(ctx, PricingInput{Model: "model001", Group: group, GroupID: &group.ID})
			require.Equal(t, PricingSourceChannel, resolved.Source)
			require.Equal(t, BillingModePerRequest, resolved.Mode)
			cost, err := billing.CalculateCostUnified(CostInput{Ctx: ctx, Model: "model001", Group: group, GroupID: &group.ID, RequestCount: 1, Resolver: resolver})
			require.NoError(t, err)
			require.Equal(t, 2.0, cost.ActualCost)
			reserved, priced := (inflightEstimateDeps{billing: billing, resolver: resolver}).estimate(ctx, key, InflightEstimateRequest{Model: "model001", Kind: InflightEstimatePerRequest})
			require.True(t, priced)
			require.Equal(t, 2.0, reserved)
		})
	}
}

func TestPricingPartialGroupCardInheritsUnsetChannelFields(t *testing.T) {
	group := &Group{ID: 100, Platform: PlatformAnthropic, RateMultiplier: 1, ModelPricing: []ChannelModelPricing{{
		Platform: PlatformAnthropic, Models: []string{"claude-sonnet-4"}, BillingMode: BillingModeToken, InputPrice: billingFloat(1e-6),
	}}}
	resolver := newResolverWithChannel(t, []ChannelModelPricing{{Platform: PlatformAnthropic, Models: []string{"claude-sonnet-4"}, BillingMode: BillingModeToken,
		InputPrice: billingFloat(10e-6), OutputPrice: billingFloat(20e-6), CacheReadPrice: billingFloat(2e-6), FastMultiplier: billingFloat(3)}})
	resolved := resolver.Resolve(context.Background(), PricingInput{Model: "claude-sonnet-4", Group: group, GroupID: &group.ID})
	require.Equal(t, PricingSourceGroup, resolved.Source)
	require.Equal(t, 1e-6, resolved.BasePricing.InputPricePerToken)
	require.Equal(t, 20e-6, resolved.BasePricing.OutputPricePerToken)
	require.Equal(t, 2e-6, resolved.BasePricing.CacheReadPricePerToken)
	require.Equal(t, 3.0, *resolved.BasePricing.FastMultiplier)
	cost, err := resolver.billingService.CalculateCostUnified(CostInput{Ctx: context.Background(), Model: "claude-sonnet-4", Group: group, GroupID: &group.ID,
		Tokens: UsageTokens{InputTokens: 1_000_000, OutputTokens: 1_000_000}, RateMultiplier: 1, Resolver: resolver})
	require.NoError(t, err)
	require.InDelta(t, 21.0, cost.ActualCost, 1e-12, "$1 explicit group input + $20 inherited channel output")
}

func TestPricingConcurrentResolutionDoesNotMutateSharedGroupOrChannel(t *testing.T) {
	group := &Group{ID: 100, Platform: PlatformAnthropic, RateMultiplier: 1, ModelPricing: []ChannelModelPricing{{
		Platform: PlatformAnthropic, Models: []string{"claude-sonnet-4"}, InputPrice: billingFloat(1e-6),
	}}}
	resolver := newResolverWithChannel(t, []ChannelModelPricing{{Platform: PlatformAnthropic, Models: []string{"claude-sonnet-4"}, BillingMode: BillingModeToken,
		InputPrice: billingFloat(10e-6), OutputPrice: billingFloat(20e-6)}})
	var workers sync.WaitGroup
	results := make(chan *ResolvedPricing, 16)
	for range 16 {
		workers.Add(1)
		go func() {
			defer workers.Done()
			results <- resolver.Resolve(context.Background(), PricingInput{Model: "claude-sonnet-4", Group: group, GroupID: &group.ID})
		}()
	}
	workers.Wait()
	close(results)
	for resolved := range results {
		require.Equal(t, BillingModeToken, resolved.Mode)
		require.Equal(t, 1e-6, resolved.BasePricing.InputPricePerToken)
		require.Equal(t, 20e-6, resolved.BasePricing.OutputPricePerToken)
	}
	require.Empty(t, group.ModelPricing[0].BillingMode, "inherited mode belongs only to the resolved clone")
	require.Nil(t, group.ModelPricing[0].OutputPrice)
	require.Equal(t, 1e-6, *group.ModelPricing[0].InputPrice)
	channel := resolver.channelService.GetChannelModelPricing(context.Background(), group.ID, "claude-sonnet-4")
	require.Equal(t, 10e-6, *channel.InputPrice)
	require.Equal(t, 20e-6, *channel.OutputPrice)
}

func TestPricingEmptyAndMultiplierOnlyCardsCannotInventFreeUnknownModel(t *testing.T) {
	for _, tc := range []struct {
		name string
		card ChannelModelPricing
	}{
		{"empty", ChannelModelPricing{BillingMode: BillingModeToken}},
		{"service_multiplier_only", ChannelModelPricing{BillingMode: BillingModeToken, FastMultiplier: billingFloat(2)}},
		{"context_multiplier_only", ChannelModelPricing{BillingMode: BillingModeToken, Intervals: []PricingInterval{{MinTokens: 0, InputMultiplier: billingFloat(2)}}}},
		{"empty_request", ChannelModelPricing{BillingMode: BillingModePerRequest}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			group := &Group{ID: 77, Platform: PlatformOpenAI, RateMultiplier: 1}
			tc.card.Platform, tc.card.Models = PlatformOpenAI, []string{"no-price-evidence-model"}
			group.ModelPricing = []ChannelModelPricing{tc.card}
			billing := NewBillingService(&config.Config{}, nil)
			resolver := NewModelPricingResolver(nil, billing)
			ctx := WithResolvedTargetPlatform(context.Background(), PlatformOpenAI)
			cost, err := billing.CalculateCostUnified(CostInput{Ctx: ctx, Model: "no-price-evidence-model", Group: group, GroupID: &group.ID,
				Tokens: UsageTokens{InputTokens: 10, OutputTokens: 10}, RequestCount: 1, RateMultiplier: 1, Resolver: resolver})
			require.ErrorIs(t, err, ErrModelPricingUnavailable)
			require.Nil(t, cost)
			for _, base := range []float64{0, 1} {
				group.RateMultiplier = base
				key := &APIKey{Group: group, GroupID: &group.ID, User: &User{ID: 1}}
				ctx := withBillingPricingSnapshot(ctx, key, time.Now(), billing, nil, nil, "no-price-evidence-model")
				amount, priced := (inflightEstimateDeps{billing: billing, resolver: resolver}).estimate(ctx, key, InflightEstimateRequest{Model: "no-price-evidence-model"})
				require.False(t, priced, "even a zero factor cannot supply missing unit-price evidence")
				require.Zero(t, amount)
			}
		})
	}
}

func TestPricingExplicitZeroRequestTierDoesNotFallThroughToPaidPrice(t *testing.T) {
	for _, tc := range []struct {
		name, size string
		tokens     int
		tier       PricingInterval
	}{
		{"label", "free-size", 100, PricingInterval{TierLabel: "free-size", MinTokens: 1000, PerRequestPrice: billingFloat(0)}},
		{"context", "", 100, PricingInterval{MinTokens: 0, MaxTokens: testPtrInt(1000), PerRequestPrice: billingFloat(0)}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			billing := NewBillingService(&config.Config{}, nil)
			resolver := NewModelPricingResolver(nil, billing)
			resolved := &ResolvedPricing{Mode: BillingModePerRequest, DefaultPerRequestPrice: 2, RequestTiers: []PricingInterval{tc.tier}}
			cost, err := billing.CalculateCostUnified(CostInput{Ctx: context.Background(), Model: "explicit-zero", Tokens: UsageTokens{InputTokens: tc.tokens}, SizeTier: tc.size,
				RequestCount: 1, RateMultiplier: 6, Resolver: resolver, Resolved: resolved})
			require.NoError(t, err)
			require.Zero(t, cost.ActualCost)
			require.Zero(t, cost.TotalCost)
		})
	}
	// A request-mode object with no declared unit price is distinct from explicit zero.
	billing := NewBillingService(&config.Config{}, nil)
	resolver := NewModelPricingResolver(nil, billing)
	cost, err := billing.CalculateCostUnified(CostInput{Ctx: context.Background(), Model: "missing-request-price", RequestCount: 1, RateMultiplier: 1,
		Resolver: resolver, Resolved: &ResolvedPricing{Mode: BillingModePerRequest}})
	require.ErrorIs(t, err, ErrModelPricingUnavailable)
	require.Nil(t, cost)
}
