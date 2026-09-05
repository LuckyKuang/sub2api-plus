package openai

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDefaultModelsIncludeBareGPT56Alias(t *testing.T) {
	require.Contains(t, DefaultModelIDs(), "gpt-5.6")
}

func TestDefaultModelsListLatestFlagshipFirst(t *testing.T) {
	require.NotEmpty(t, DefaultModels)
	require.Equal(t, "gpt-6-astra", DefaultModels[0].ID)
}

func TestDefaultModelsExposeOnlyCanonicalGPT6Astra(t *testing.T) {
	require.Contains(t, DefaultModelIDs(), "gpt-6-astra")
	require.NotContains(t, DefaultModelIDs(), "gpt-6")
}
