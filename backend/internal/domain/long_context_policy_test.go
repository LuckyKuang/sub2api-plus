//go:build unit

package domain

import (
	"math"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLongContextRulePrecedenceAndDeepClone(t *testing.T) {
	zero := 0.0
	policy := BillingRatePolicy{LongContextPlatforms: map[string]int{"openai": 100}, LongContextModels: []LongContextModelRule{{Platform: "openai", Model: "public", Threshold: 0, Mode: "prices", Input: &zero, Output: &zero}}}
	require.NoError(t, policy.Validate())
	rule, ok := policy.LongContextRule("openai", "PUBLIC")
	require.True(t, ok)
	require.Zero(t, rule.Threshold)
	clone := policy.Clone()
	*clone.LongContextModels[0].Input = 20
	clone.LongContextPlatforms["openai"] = 50
	require.Zero(t, *policy.LongContextModels[0].Input)
	require.Equal(t, 100, policy.LongContextPlatforms["openai"])
	_, ok = policy.LongContextRule("anthropic", "public")
	require.False(t, ok)
}
func TestLongContextRejectsInvalidConfig(t *testing.T) {
	for _, value := range []float64{-1, math.NaN(), math.Inf(1)} {
		policy := BillingRatePolicy{LongContextModels: []LongContextModelRule{{Platform: "openai", Model: "model", Threshold: 10, Mode: "prices", Input: &value, Output: &value}}}
		require.Error(t, policy.Validate())
	}
	for _, model := range []string{"model*", "has space", "has\u00a0space"} {
		require.Error(t, (BillingRatePolicy{LongContextModels: []LongContextModelRule{{Platform: "openai", Model: model, Threshold: 0}}}).Validate())
	}
	require.Error(t, (BillingRatePolicy{LongContextModels: []LongContextModelRule{{Platform: "openai", Model: "model", Mode: "prices", Threshold: 100}}}).Validate())
}
