package openai

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDefaultModelsIncludeBareGPT56Alias(t *testing.T) {
	require.Contains(t, DefaultModelIDs(), "gpt-5.6")
}

func TestDefaultModelsIncludeGPT6Astra(t *testing.T) {
	require.Contains(t, DefaultModelIDs(), "gpt-6-astra")
	require.NotContains(t, DefaultModelIDs(), "gpt-6")
	var displayName string
	for _, model := range DefaultModels {
		if model.ID == "gpt-6-astra" {
			displayName = model.DisplayName
			break
		}
	}
	require.Equal(t, "GPT-6 Astra", displayName)
}

func TestDefaultModelsListLatestFlagshipFirst(t *testing.T) {
	require.NotEmpty(t, DefaultModels)
	require.Equal(t, "gpt-6-astra", DefaultModels[0].ID)
}

func TestDefaultModelIDsAreUnique(t *testing.T) {
	seen := make(map[string]struct{}, len(DefaultModels))
	for _, model := range DefaultModels {
		require.NotContains(t, seen, model.ID)
		seen[model.ID] = struct{}{}
	}
}

func TestDefaultModelsIncludeGPTImage25(t *testing.T) {
	require.Contains(t, DefaultModelIDs(), "gpt-image-2.5-flare")
	require.Contains(t, DefaultModelIDs(), "gpt-image-2.5-sunburst")
}
