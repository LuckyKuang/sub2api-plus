//go:build unit || !integration

package service

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// A provider that reports a partial (or missing) TTL breakdown still owes for
// every cache-write token it declared. The unclassified remainder is billed in
// the 5m class, so a 3k detail out of an 11k aggregate must not leave 8k free.
func TestComputeCacheCreationCost_ChargesUnclassifiedRemainder(t *testing.T) {
	svc := &BillingService{}
	pricing := &ModelPricing{SupportsCacheBreakdown: true, CacheCreation5mPrice: 0.002, CacheCreation1hPrice: 0.004}

	tests := []struct {
		name     string
		tokens   UsageTokens
		wantCost float64
	}{
		{"5m detail then aggregate-only delta", UsageTokens{CacheCreationTokens: 11000, CacheCreation5mTokens: 3000}, 22},
		{"1h detail does not imply a 1h remainder", UsageTokens{CacheCreationTokens: 11000, CacheCreation1hTokens: 3000}, 28},
		{"mixed detail does not imply a proportional remainder", UsageTokens{CacheCreationTokens: 11000, CacheCreation5mTokens: 1000, CacheCreation1hTokens: 2000}, 26},
		{"no details", UsageTokens{CacheCreationTokens: 11000}, 22},
		{"negative detail clamps but keeps the remainder", UsageTokens{CacheCreationTokens: 11000, CacheCreation5mTokens: -1, CacheCreation1hTokens: 3000}, 28},
		{"contradictory detail keeps the ratio cap", UsageTokens{CacheCreationTokens: 100, CacheCreation5mTokens: 50, CacheCreation1hTokens: 200}, 0.36},
		{"detail-only snapshot stays as reported", UsageTokens{CacheCreation5mTokens: 20, CacheCreation1hTokens: 30}, 0.16},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			before := tt.tokens
			for _, multiplier := range []float64{1, 2} {
				require.InDelta(t, tt.wantCost*multiplier, svc.computeCacheCreationCost(pricing, tt.tokens, 0, multiplier, 1), 1e-12)
			}
			require.Equal(t, before, tt.tokens, "allocation must not mutate the usage")
		})
	}
}

// Without breakdown pricing every write is charged at the single cache-write
// price, including the unclassified remainder.
func TestComputeCacheCreationCost_FlatPriceChargesEveryWrite(t *testing.T) {
	svc := &BillingService{}
	pricing := &ModelPricing{}
	tokens := UsageTokens{CacheCreationTokens: 11000, CacheCreation5mTokens: 3000}
	require.InDelta(t, 11000*0.003, svc.computeCacheCreationCost(pricing, tokens, 0.003, 1, 1), 1e-12)
}

func TestAllocateCacheCreationBillingTokens(t *testing.T) {
	tests := []struct {
		name   string
		tokens UsageTokens
		want5m int
		want1h int
	}{
		{"remainder_goes_to_5m", UsageTokens{CacheCreationTokens: 200, CacheCreation5mTokens: 60}, 200, 0},
		{"1h_detail_kept", UsageTokens{CacheCreationTokens: 200, CacheCreation1hTokens: 60}, 140, 60},
		{"no_details_all_5m", UsageTokens{CacheCreationTokens: 200}, 200, 0},
		{"complete_split_unchanged", UsageTokens{CacheCreationTokens: 200, CacheCreation5mTokens: 50, CacheCreation1hTokens: 150}, 50, 150},
		{"detail_only_unchanged", UsageTokens{CacheCreation5mTokens: 40, CacheCreation1hTokens: 60}, 40, 60},
		{"ratio_cap_then_no_remainder", UsageTokens{CacheCreationTokens: 120, CacheCreation5mTokens: 80, CacheCreation1hTokens: 160}, 40, 80},
		{"zero", UsageTokens{}, 0, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got5m, got1h := allocateCacheCreationBillingTokens(tt.tokens)
			require.Equal(t, tt.want5m, got5m)
			require.Equal(t, tt.want1h, got1h)
			if tt.tokens.CacheCreationTokens > 0 {
				require.Equal(t, tt.tokens.CacheCreationTokens, got5m+got1h, "every declared write is billed")
			}
		})
	}
}

// Account statistics price the same quantity, so a partial breakdown must not
// under-report there either.
func TestCalculateTokenStatsCost_ChargesUnclassifiedRemainder(t *testing.T) {
	pricing := &ChannelModelPricing{
		BillingMode:       BillingModeToken,
		CacheWritePrice:   testPtrFloat64(0.003),
		CacheWrite1hPrice: testPtrFloat64(0.005),
	}

	tests := []struct {
		name     string
		tokens   UsageTokens
		wantCost float64
	}{
		{"5m_remainder", UsageTokens{CacheCreationTokens: 200, CacheCreation5mTokens: 60}, 0.6},
		{"1h_remainder", UsageTokens{CacheCreationTokens: 200, CacheCreation1hTokens: 60}, 0.72},
		{"mixed_remainder", UsageTokens{CacheCreationTokens: 200, CacheCreation5mTokens: 40, CacheCreation1hTokens: 60}, 0.72},
		{"no_details", UsageTokens{CacheCreationTokens: 200}, 0.6},
		{"complete_5m", UsageTokens{CacheCreationTokens: 200, CacheCreation5mTokens: 200}, 0.6},
		{"complete_1h", UsageTokens{CacheCreationTokens: 200, CacheCreation1hTokens: 200}, 1.0},
		{"zero_aggregate_keeps_details", UsageTokens{CacheCreation5mTokens: 40, CacheCreation1hTokens: 60}, 0.42},
		{"ratio_cap", UsageTokens{CacheCreationTokens: 120, CacheCreation5mTokens: 80, CacheCreation1hTokens: 160}, 0.52},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := calculateTokenStatsCost(pricing, tt.tokens)
			require.NotNil(t, result)
			require.InDelta(t, tt.wantCost, *result, 1e-12)
		})
	}

	require.Nil(t, calculateTokenStatsCost(pricing, UsageTokens{}), "no tokens means no cost")
}
