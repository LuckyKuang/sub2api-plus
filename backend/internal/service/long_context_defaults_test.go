//go:build unit

package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

// The editor must expose real catalog thresholds rather than inventing one
// number per supplier. Missing tiers are visibly zero; inclusive is preserved.
func TestLongContextDefaultsExposeCatalogValues(t *testing.T) {
	svc := newReferenceService(t, `{
	 "gpt-6-sol":{"litellm_provider":"openai","input_cost_per_token":0.000002,"output_cost_per_token":0.00001,"long_context_input_token_threshold":272000,"long_context_input_cost_multiplier":2},
	 "grok-4.20":{"litellm_provider":"xai","input_cost_per_token":0.000002,"output_cost_per_token":0.000006,"long_context_input_token_threshold":200000,"long_context_input_cost_multiplier":2},
	 "deepseek-v4-flash":{"litellm_provider":"deepseek","input_cost_per_token":0.000001,"output_cost_per_token":0.000002}
	}`)
	defaults, err := svc.LongContextDefaults(context.Background())
	require.NoError(t, err)
	find := func(platform, model string) LongContextDefault {
		for _, row := range defaults[platform] {
			if row.Model == model {
				return row
			}
		}
		t.Fatalf("missing default for %s/%s", platform, model)
		return LongContextDefault{}
	}
	require.Equal(t, LongContextDefault{Model: "gpt-6-sol", Threshold: 272000}, find(PlatformOpenAI, "gpt-6-sol"))
	require.Equal(t, LongContextDefault{Model: "grok-4.20", Threshold: 200000, Inclusive: true}, find(PlatformGrok, "grok-4.20"))
	require.Zero(t, find(PlatformDeepseek, "deepseek-v4-flash").Threshold)
	for _, platform := range svc.SupportedPlatforms() {
		require.Contains(t, defaults, platform)
	}
}
