//go:build unit

package service

import (
	"context"
	"testing"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/domain"
	"github.com/stretchr/testify/require"
)

// Requirements: explicit zero wins over account enable; a positive replacement
// re-enables tiering; exact public-model overrides win over supplier defaults.
func TestCompositeLongContextExplicitZeroAndReenable(t *testing.T) {
	bs, resolver := newTokenCostTestEnv(t, PlatformComposite, nil, geminiLadderCatalogStub(t))
	group := &Group{ID: 100, Platform: PlatformComposite, LongContextPricingEnabled: true, BillingRatePolicy: domain.BillingRatePolicy{LongContextPlatforms: map[string]int{PlatformGemini: 0}}}
	ctx := WithCompositeRouteDecision(context.Background(), CompositeRouteDecision{Matched: true, PublicModel: "public-gemini", UpstreamModel: "gemini-2.5-pro", TargetPlatform: PlatformGemini})
	enabled := true
	calculate := func(input, read int) *CostBreakdown {
		resolved := resolver.Resolve(ctx, PricingInput{Model: "gemini-2.5-pro", Group: group, GroupID: &group.ID})
		got, err := bs.CalculateCostUnified(CostInput{Ctx: ctx, Model: "gemini-2.5-pro", Group: group, GroupID: &group.ID, Tokens: UsageTokens{InputTokens: input, CacheReadTokens: read, OutputTokens: 10}, RateMultiplier: 1, Resolver: resolver, Resolved: resolved, LongContextBillingEnabled: &enabled})
		require.NoError(t, err)
		return got
	}
	require.InDelta(t, .375, calculate(300000, 0).InputCost, 1e-9)
	group.BillingRatePolicy.LongContextModels = []domain.LongContextModelRule{{Platform: PlatformGemini, Model: "public-gemini", Threshold: 100000}}
	require.InDelta(t, .0625, calculate(50000, 50000).InputCost, 1e-9) // exactly T stays in base tier
	require.InDelta(t, .125, calculate(50000, 50001).InputCost, 1e-9)  // normalized input includes cache
	schedule, err := bs.ResolveContextPricingSchedule(ctx, resolver, ContextPricingScheduleInput{Model: "gemini-2.5-pro", Group: group, Platform: PlatformGemini})
	require.NoError(t, err)
	require.Len(t, schedule.Tiers, 2)
	require.Equal(t, 100000, *schedule.Tiers[0].MaxTokens)
}

// Composite no longer has one global switch: an absent platform/model rule
// inherits that model's preset even on an existing group with the old flag off.
func TestCompositeLongContextDefaultInheritsModelPreset(t *testing.T) {
	bs, resolver := newTokenCostTestEnv(t, PlatformComposite, nil, geminiLadderCatalogStub(t))
	group := &Group{ID: 100, Platform: PlatformComposite, LongContextPricingEnabled: false}
	ctx := WithResolvedTargetPlatform(context.Background(), PlatformGemini)
	got, err := bs.CalculateTokenCostForRequest(TokenCostRequest{Ctx: ctx, Model: "gemini-2.5-pro", Group: group, Tokens: UsageTokens{InputTokens: 300000}, RateMultiplier: 1, Resolver: resolver})
	require.NoError(t, err)
	require.InDelta(t, .75, got.InputCost, 1e-9)
}

// Requirements: custom absolute prices are USD/million, zeros are intentional,
// no guessed surcharge; read/write and output prices apply to the whole request.
func TestCompositeLongContextCustomPricesAndMultipliers(t *testing.T) {
	bs, resolver := newTokenCostTestEnv(t, PlatformComposite, []ChannelModelPricing{{Platform: PlatformGemini, Models: []string{"gemini-2.5-pro"}, BillingMode: BillingModeToken, InputPrice: testPtrFloat64(2e-6), OutputPrice: testPtrFloat64(4e-6), CacheWritePrice: testPtrFloat64(3e-6), CacheWrite1hPrice: testPtrFloat64(6e-6), CacheReadPrice: testPtrFloat64(1e-6)}}, geminiCatalogStub())
	group := &Group{ID: 100, Platform: PlatformComposite, BillingRatePolicy: domain.BillingRatePolicy{LongContextModels: []domain.LongContextModelRule{{Platform: PlatformGemini, Model: "gemini-2.5-pro", Threshold: 100, Mode: "prices", Input: testPtrFloat64(10), Output: testPtrFloat64(20), CacheWrite: testPtrFloat64(30), CacheRead: testPtrFloat64(0)}}}}
	ctx := WithResolvedTargetPlatform(context.Background(), PlatformGemini)
	got, err := bs.CalculateTokenCostForRequest(TokenCostRequest{Ctx: ctx, Model: "gemini-2.5-pro", Group: group, Tokens: UsageTokens{InputTokens: 50, CacheCreationTokens: 30, CacheReadTokens: 21, OutputTokens: 10}, RateMultiplier: 1, Resolver: resolver})
	require.NoError(t, err)
	require.InDelta(t, .0005, got.InputCost, 1e-10)
	require.InDelta(t, .0002, got.OutputCost, 1e-10)
	require.InDelta(t, .0009, got.CacheCreationCost, 1e-10)
	require.Zero(t, got.CacheReadCost)
	rule := &group.BillingRatePolicy.LongContextModels[0]
	rule.Mode = "multipliers"
	rule.Input = testPtrFloat64(2)
	rule.Output = testPtrFloat64(3)
	rule.CacheWrite = testPtrFloat64(4)
	rule.CacheRead = testPtrFloat64(5)
	rule.CacheWrite1h = testPtrFloat64(6)
	resolved := resolver.Resolve(ctx, PricingInput{Model: "gemini-2.5-pro", Group: group, GroupID: &group.ID})
	high := resolver.GetIntervalPricing(resolved, 101)
	require.InDelta(t, 4e-6, high.InputPricePerToken, 1e-12)
	require.InDelta(t, 12e-6, high.OutputPricePerToken, 1e-12)
	require.InDelta(t, 36e-6, high.CacheCreation1hPrice, 1e-12)
	require.InDelta(t, 5e-6, high.CacheReadPricePerToken, 1e-12)
}

func TestCompositeLongContextMissingHighPriceIsError(t *testing.T) {
	bs, resolver := newTokenCostTestEnv(t, PlatformGemini, nil, geminiCatalogStub())
	group := &Group{ID: 100, Platform: PlatformGemini, BillingRatePolicy: domain.BillingRatePolicy{LongContextPlatforms: map[string]int{PlatformGemini: 100}}}
	_, err := bs.CalculateTokenCostForRequest(TokenCostRequest{Ctx: context.Background(), Model: "gemini-2.5-pro", Group: group, Tokens: UsageTokens{InputTokens: 101}, RateMultiplier: 1, Resolver: resolver})
	require.ErrorContains(t, err, "requires high-tier prices")
}

// Requirements: changing a first threshold must preserve every later step and
// inherited official inclusive boundaries; conflicting bounds are unavailable.
func TestCompositeLongContextRetainsLaterTiersAndBoundaries(t *testing.T) {
	bs, resolver := newTokenCostTestEnv(t, PlatformGemini, []ChannelModelPricing{{Platform: PlatformGemini, Models: []string{"gemini-2.5-pro"}, BillingMode: BillingModeToken, Intervals: []PricingInterval{{MinTokens: 0, MaxTokens: testPtrInt(100), InputPrice: testPtrFloat64(1e-6)}, {MinTokens: 100, MaxTokens: testPtrInt(200), InputPrice: testPtrFloat64(2e-6)}, {MinTokens: 200, InputPrice: testPtrFloat64(3e-6)}}}}, geminiCatalogStub())
	_ = bs
	group := &Group{ID: 100, Platform: PlatformGemini, BillingRatePolicy: domain.BillingRatePolicy{LongContextPlatforms: map[string]int{PlatformGemini: 150}}}
	resolved := resolver.Resolve(context.Background(), PricingInput{Model: "gemini-2.5-pro", Group: group, GroupID: &group.ID})
	require.Nil(t, resolved.longContextPricingError)
	require.Len(t, resolved.Intervals, 3)
	require.InDelta(t, 1e-6, resolver.GetIntervalPricing(resolved, 150).InputPricePerToken, 1e-12)
	require.InDelta(t, 2e-6, resolver.GetIntervalPricing(resolved, 151).InputPricePerToken, 1e-12)
	require.InDelta(t, 3e-6, resolver.GetIntervalPricing(resolved, 201).InputPricePerToken, 1e-12)
	group.BillingRatePolicy.LongContextPlatforms[PlatformGemini] = 200
	require.Error(t, resolver.Resolve(context.Background(), PricingInput{Model: "gemini-2.5-pro", Group: group, GroupID: &group.ID}).longContextPricingError)
}

func TestCompositeLongContextSnapshotPinsPolicyAndPreflightsMissingPrices(t *testing.T) {
	bs, resolver := newTokenCostTestEnv(t, PlatformGemini, nil, geminiLadderCatalogStub(t))
	group := &Group{ID: 100, Platform: PlatformGemini, RateMultiplier: 1, BillingPolicyVersion: 2, BillingRatePolicy: domain.BillingRatePolicy{LongContextPlatforms: map[string]int{PlatformGemini: 0}}}
	key := &APIKey{ID: 1, GroupID: &group.ID, Group: group}
	ctx := withBillingPricingSnapshot(context.Background(), key, time.Now(), bs, resolver.channelService, nil, "gemini-2.5-pro")
	require.NoError(t, ValidateBillingPricingSnapshot(ctx))
	group.BillingRatePolicy.LongContextPlatforms[PlatformGemini] = 100000
	got, err := bs.CalculateTokenCostForRequest(TokenCostRequest{Ctx: ctx, Model: "gemini-2.5-pro", Group: group, Tokens: UsageTokens{InputTokens: 300000}, RateMultiplier: 1, Resolver: resolver})
	require.NoError(t, err)
	require.InDelta(t, .375, got.InputCost, 1e-9)
	bs2, resolver2 := newTokenCostTestEnv(t, PlatformGemini, nil, geminiCatalogStub())
	missing := withBillingPricingSnapshot(context.Background(), key, time.Now(), bs2, resolver2.channelService, nil, "gemini-2.5-pro")
	require.ErrorContains(t, ValidateBillingPricingSnapshot(missing), "requires high-tier prices")
}
