//go:build unit || !integration

package openai

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

// Requirements come from openai/codex 126a0ee92ed2c5e73cd8a8f7e37563e8527552d7,
// codex-rs/models-manager/models.json; expected values are literal source facts.
func TestGPT61TerraOfficialIdentityAndMetadata(t *testing.T) {
	require.Contains(t, DefaultModelIDs(), "gpt-6.1-terra")
	for _, model := range []string{"gpt-6.1-terra", "openai/gpt-6.1-terra-max", "GPT_6.1_TERRA", "gpt-6.1-terra-openai-compact"} {
		require.True(t, IsGPT61TerraModelSpelling(model), model)
		require.False(t, IsGPT61SolModelSpelling(model), model)
	}
	for _, model := range []string{"gpt-6.1", "gpt-6.1-terrain", "gpt-6.1-terra-preview", "gpt-6.1-terra-ultra", "gpt-5.6-terra"} {
		require.False(t, IsGPT61TerraModelSpelling(model), model)
	}
	for _, effort := range []string{"none", "minimal"} {
		require.Error(t, ValidateGPT61ReasoningEffort("gpt-6.1-terra", effort))
	}
	for _, effort := range []string{"", "low", "medium", "high", "xhigh", "max"} {
		require.NoError(t, ValidateGPT61ReasoningEffort("gpt-6.1-terra", effort))
	}
	var metadata struct {
		Slug                     string `json:"slug"`
		DisplayName              string `json:"display_name"`
		ContextWindow            int    `json:"context_window"`
		MaxContextWindow         int    `json:"max_context_window"`
		DefaultReasoningLevel    string `json:"default_reasoning_level"`
		SupportedReasoningLevels []struct {
			Effort string `json:"effort"`
		} `json:"supported_reasoning_levels"`
	}
	require.NoError(t, json.Unmarshal(CodexGPT61TerraMetadata, &metadata))
	require.Equal(t, "gpt-6.1-terra", metadata.Slug)
	require.Equal(t, "GPT-6.1 Terra", metadata.DisplayName)
	require.Equal(t, 500_000, metadata.ContextWindow)
	require.Equal(t, 872_000, metadata.MaxContextWindow)
	require.Equal(t, "low", metadata.DefaultReasoningLevel)
	var levels []string
	for _, level := range metadata.SupportedReasoningLevels {
		levels = append(levels, level.Effort)
	}
	require.Equal(t, []string{"low", "medium", "high", "xhigh", "max", "ultra"}, levels)
	require.Contains(t, CodexBaseInstructionsForModel("gpt-6.1-terra"), "based on GPT-6")
}
