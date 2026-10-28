//go:build unit

package service

import (
	"context"
	"testing"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/domain"
	"github.com/stretchr/testify/require"
)

// Reservation must include all configured selling factors that settlement
// applies to the same known request, so parallel requests cannot consume the
// balance using a lower preflight price. These amounts come from the public
// business contract: $2 x platform 2 x model 3 x high effort 4 = $48 per unit.
func TestInflightRequestEffortReserveIncludesAcceptedSellingFactors(t *testing.T) {
	for _, mode := range []BillingMode{BillingModePerRequest, BillingModeImage, BillingModeVideo} {
		t.Run(string(mode), func(t *testing.T) {
			svc := newInflightEstimateGateway(t, nil)
			group := &Group{ID: 77, Platform: PlatformComposite, RateMultiplier: 1,
				BillingRatePolicy: domain.BillingRatePolicy{
					PlatformMultipliers: map[string]float64{PlatformOpenAI: 2},
					ModelMultipliers:    []domain.BillingModelRate{{Platform: PlatformOpenAI, Model: "model001", Multiplier: 3}},
				},
				ModelPricing: []ChannelModelPricing{{Platform: PlatformOpenAI, Models: []string{"model001"},
					BillingMode: mode, PerRequestPrice: billingFloat(2), ReasoningEffortMultipliers: map[string]float64{"high": 4}}},
			}
			key := &APIKey{ID: 11, GroupID: &group.ID, Group: group, User: &User{ID: 22}}
			ctx := svc.WithBillingPricingSnapshot(WithResolvedTargetPlatform(context.Background(), PlatformOpenAI), key, time.Now(), "model001")
			req := InflightEstimateRequest{Model: "model001", ReasoningEffort: "high", Units: 2}
			expected := 96.0
			switch mode {
			case BillingModePerRequest:
				req.Kind = InflightEstimatePerRequest
			case BillingModeImage:
				req.Kind = InflightEstimateImage
			case BillingModeVideo:
				req.Kind = InflightEstimateVideo
				req.VideoDurationSeconds = 5
				expected = 480 // Two five-second videos, ten independently priced seconds.
			}
			reserved, priced := svc.EstimateInflightReservation(ctx, key, req)
			require.True(t, priced)
			require.InDelta(t, expected, reserved, 1e-12, "the hold must include the configured effort factor before upstream work")
			settled, err := svc.billingService.CalculateCostUnified(CostInput{Ctx: ctx, Model: "model001", Group: group, GroupID: &group.ID,
				RequestCount: 2, UsageUnits: map[BillingMode]float64{BillingModeVideo: 10}[mode], ReasoningEffort: "high", Resolver: svc.resolver})
			require.NoError(t, err)
			require.InDelta(t, expected, settled.ActualCost, 1e-12)
		})
	}
}

func TestInflightAudioRequestEffortAndDefaultPriceBoundaries(t *testing.T) {
	for _, tc := range []struct {
		name, effort    string
		price, expected float64
	}{
		{"high", "high", 2, 96},
		{"reduced factor", "low", 2, 12},
		{"missing effort", "", 2, 24},
		{"unconfigured effort", "medium", 2, 24},
		{"explicit free", "high", 0, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc := newInflightEstimateGateway(t, nil)
			group := &Group{ID: 77, Platform: PlatformComposite, RateMultiplier: 1,
				BillingRatePolicy: domain.BillingRatePolicy{
					PlatformMultipliers: map[string]float64{PlatformGrok: 2},
					ModelMultipliers:    []domain.BillingModelRate{{Platform: PlatformGrok, Model: "voice-model", Multiplier: 3}},
				},
				ModelPricing: []ChannelModelPricing{{Platform: PlatformGrok, Models: []string{"voice-model"},
					BillingMode: BillingModePerRequest, PerRequestPrice: &tc.price,
					ReasoningEffortMultipliers: map[string]float64{"high": 4, "low": 0.5}}},
			}
			key := &APIKey{ID: 11, GroupID: &group.ID, Group: group, User: &User{ID: 22}}
			ctx := svc.WithBillingPricingSnapshot(WithResolvedTargetPlatform(context.Background(), PlatformGrok), key, time.Now(), "voice-model")
			amount, priced := svc.EstimateInflightReservation(ctx, key, InflightEstimateRequest{
				Model: "voice-model", Kind: InflightEstimateAudio, AudioMode: "tts", AudioUnits: 2, ReasoningEffort: tc.effort,
			})
			require.True(t, priced)
			require.InDelta(t, tc.expected, amount, 1e-12)
		})
	}
}

func TestInflightTextRequestReservesKnownTierAndEffortAtAcceptedRate(t *testing.T) {
	svc := newInflightEstimateGateway(t, nil)
	group := &Group{ID: 77, Platform: PlatformOpenAI, RateMultiplier: 2,
		ModelPricing: []ChannelModelPricing{{Models: []string{"model001"}, BillingMode: BillingModeToken,
			InputPrice: billingFloat(0), OutputPrice: billingFloat(2), FastMultiplier: billingFloat(3),
			ReasoningEffortMultipliers: map[string]float64{"high": 4}}},
	}
	key := &APIKey{ID: 11, GroupID: &group.ID, Group: group, User: &User{ID: 22}}
	ctx := svc.WithBillingPricingSnapshot(context.Background(), key, time.Now(), "model001")
	req := (InflightEstimateRequest{Protocol: ContentModerationProtocolOpenAIResponses, Model: "model001", Kind: InflightEstimateToken, MaxTokens: 1}).WithRequestPricingMetadata([]byte(`{"service_tier":"priority","reasoning":{"effort":"high"}}`))
	amount, priced := svc.EstimateInflightReservation(ctx, key, req)
	require.True(t, priced)
	// One output token at $2, selling rate 2, priority 3, high effort 4.
	require.InDelta(t, 48.0, amount, 1e-12)
}
