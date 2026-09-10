package service

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNormalizeKnownOpenAICodexModel_BareGPT56RoutesToSol(t *testing.T) {
	tests := map[string]string{
		"gpt-5.6":            "gpt-5.6-sol",
		"openai/gpt-5.6":     "gpt-5.6-sol",
		"gpt5.6":             "gpt-5.6-sol",
		"gpt-5.6-high":       "gpt-5.6-sol",
		"gpt-5.6-max":        "gpt-5.6-sol",
		"gpt-5.6-2026-07-09": "gpt-5.6-sol",
		"openai/gpt-5.6-max": "gpt-5.6-sol",
	}

	for input, expected := range tests {
		t.Run(input, func(t *testing.T) {
			require.Equal(t, expected, normalizeKnownOpenAICodexModel(input))
		})
	}
}

func TestUsageBillingModelCandidates_BareGPT56IncludesSol(t *testing.T) {
	require.Equal(t,
		[]string{"gpt-5.6", "gpt-5.6-sol"},
		usageBillingModelCandidates("gpt-5.6"),
	)
	require.Equal(t,
		[]string{"openai/gpt-5.6", "gpt-5.6", "gpt-5.6-sol"},
		usageBillingModelCandidates("openai/gpt-5.6"),
	)
}

func TestNormalizeKnownOpenAICodexModel_GPT6AstraIsStrict(t *testing.T) {
	accepted := []string{
		"gpt-6-astra",
		"GPT-6-ASTRA",
		"openai/gpt-6-astra",
		"gpt_6_astra",
	}
	for _, input := range accepted {
		t.Run("accept_"+input, func(t *testing.T) {
			require.Equal(t, "gpt-6-astra", normalizeKnownOpenAICodexModel(input))
		})
	}

	rejected := []string{
		"gpt-6",
		"openai/gpt-6",
		"gpt-6-max",
		"gpt-6-astra-max",
		"gpt-6-astra-2026-09-04",
		"gpt-6-astra-unknown",
	}
	for _, input := range rejected {
		t.Run("reject_"+input, func(t *testing.T) {
			require.Empty(t, normalizeKnownOpenAICodexModel(input))
		})
	}
}

func TestUsageBillingModelCandidates_GPT6AstraDoesNotClaimAliases(t *testing.T) {
	require.Equal(t, []string{"openai/gpt-6-astra", "gpt-6-astra"}, usageBillingModelCandidates("openai/gpt-6-astra"))
	require.Equal(t, []string{"gpt-6"}, usageBillingModelCandidates("gpt-6"))
}
