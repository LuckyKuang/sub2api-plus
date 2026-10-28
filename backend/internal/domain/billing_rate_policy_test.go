//go:build unit

package domain

import (
	"math"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestBillingRatePolicyRulePrecedenceAndUnits(t *testing.T) {
	policy := BillingRatePolicy{PlatformMultipliers: map[string]float64{"openai": 1.5, "stepfun": .7}, ModelMultipliers: []BillingModelRate{
		{Platform: "openai", Model: "code-*", Multiplier: 2},
		{Platform: "openai", Model: "code-fast*", Multiplier: 3},
		{Platform: "openai", Model: "code-fast", Multiplier: 4},
		{Platform: "openai", Model: "code-fast", BillingUnit: "image", Multiplier: 0},
	}}
	require.NoError(t, policy.Validate())
	cases := []struct {
		platform, model, unit string
		multiplier            float64
		source, pattern       string
	}{
		{"openai", "unlisted", "token", 1.5, "platform", ""},
		{"stepfun", "code-fast", "token", .7, "platform", ""},
		{"kimi", "code-fast", "token", 1, "default", ""},
		{"openai", "code-other", "token", 3, "model", "code-*"},
		{"openai", "code-faster", "token", 4.5, "model", "code-fast*"},
		{"openai", "CODE-FAST", "token", 6, "model", "code-fast"},
		{"openai", "code-fast", "image", 0, "model", "code-fast"},
	}
	for _, c := range cases {
		t.Run(c.platform+"/"+c.model+"/"+c.unit, func(t *testing.T) {
			got := policy.Resolve(c.platform, c.model, c.unit)
			require.Equal(t, c.multiplier, got.Multiplier)
			require.Equal(t, c.source, got.Source)
			require.Equal(t, c.pattern, got.Pattern)
		})
	}
	// Admin row order must never change the price the buyer is charged.
	for i, j := 0, len(policy.ModelMultipliers)-1; i < j; i, j = i+1, j-1 {
		policy.ModelMultipliers[i], policy.ModelMultipliers[j] = policy.ModelMultipliers[j], policy.ModelMultipliers[i]
	}
	require.Equal(t, 6.0, policy.Resolve("openai", "code-fast", "token").Multiplier)
	require.Zero(t, policy.Resolve("openai", "code-fast", "image").Multiplier)
}

func TestBillingRatePolicyRejectsInvalidConfiguration(t *testing.T) {
	for _, factor := range []float64{-1, math.NaN(), math.Inf(1), 1000000, .12345} {
		require.Error(t, (BillingRatePolicy{PlatformMultipliers: map[string]float64{"openai": factor}}).Validate())
		require.Error(t, (BillingRatePolicy{ModelMultipliers: []BillingModelRate{{Platform: "openai", Model: "code", Multiplier: factor}}}).Validate())
	}
	for _, factor := range []float64{0, .0001, 1.5, 999999.9999} {
		require.NoError(t, (BillingRatePolicy{PlatformMultipliers: map[string]float64{"openai": factor}}).Validate())
	}
	for _, model := range []string{"", " code", "code ", "code space", "code\u00a0space", "a*b", "a**", "code\n", strings.Repeat("x", 257), strings.Repeat("模", 86)} {
		require.Error(t, (BillingRatePolicy{ModelMultipliers: []BillingModelRate{{Platform: "openai", Model: model, Multiplier: 1}}}).Validate())
	}
	require.Error(t, (BillingRatePolicy{PlatformMultipliers: map[string]float64{"unregistered": 1}}).Validate())
	require.Error(t, (BillingRatePolicy{PlatformMultipliers: map[string]float64{"composite": 1}}).Validate())
	require.Error(t, (BillingRatePolicy{ModelMultipliers: []BillingModelRate{{Platform: "openai", Model: "code", BillingUnit: "unknown", Multiplier: 1}}}).Validate())
	require.Error(t, (BillingRatePolicy{ModelMultipliers: []BillingModelRate{{Platform: "openai", Model: "Code", Multiplier: 1}, {Platform: "openai", Model: "code", Multiplier: 2}}}).Validate())
}

func TestBillingRatePolicyEmptyInheritanceAndDetachedClone(t *testing.T) {
	require.Equal(t, BillingRateMatch{Multiplier: 1, PlatformMultiplier: 1, ModelMultiplier: 1, Source: "default"}, (BillingRatePolicy{}).Resolve("openai", "code", "token"))
	policy := BillingRatePolicy{PlatformMultipliers: map[string]float64{"openai": 0}, ModelMultipliers: []BillingModelRate{{Platform: "stepfun", Model: "step-*", Multiplier: .5}}}
	copy := policy.Clone()
	policy.PlatformMultipliers["openai"] = 2
	policy.ModelMultipliers[0].Multiplier = 3
	require.Zero(t, copy.Resolve("openai", "code", "token").Multiplier)
	require.Equal(t, .5, copy.Resolve("stepfun", "step-test", "token").Multiplier)
}

func TestBillingRatePolicyMultipliesIndependentFactorsAndPreservesZero(t *testing.T) {
	policy := BillingRatePolicy{PlatformMultipliers: map[string]float64{"openai": 1.2, "anthropic": 0}, ModelMultipliers: []BillingModelRate{
		{Platform: "openai", Model: "priced", Multiplier: 1.5},
		{Platform: "openai", Model: "free", Multiplier: 0},
		{Platform: "anthropic", Model: "priced", Multiplier: 1.5},
	}}
	for _, tc := range []struct {
		platform, model                    string
		platformFactor, modelFactor, total float64
	}{
		{"openai", "default", 1.2, 1, 1.2},
		{"openai", "priced", 1.2, 1.5, 1.8},
		{"openai", "free", 1.2, 0, 0},
		{"anthropic", "priced", 0, 1.5, 0},
		{"kimi", "priced", 1, 1, 1},
	} {
		t.Run(tc.platform+"/"+tc.model, func(t *testing.T) {
			got := policy.Resolve(tc.platform, tc.model, "token")
			require.Equal(t, tc.platformFactor, got.PlatformMultiplier)
			require.Equal(t, tc.modelFactor, got.ModelMultiplier)
			require.InDelta(t, tc.total, got.Multiplier, 1e-12)
		})
	}
}
