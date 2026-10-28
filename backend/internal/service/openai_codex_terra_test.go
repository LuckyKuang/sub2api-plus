//go:build unit || !integration

package service

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

// The Terra catalog at openai/codex 126a0ee92ed2 advertises 500k default / 872k
// maximum context, priority service and automatic-delegation ultra. API keys
// retain the gateway's standard Responses boundary rather than Responses Lite.
func TestCodexGPT61TerraCatalogAndAPIKeyBoundary(t *testing.T) {
	body, err := BuildCodexModelsManifest([]string{"gpt-6.1-terra"})
	require.NoError(t, err)
	models := decodeCodexManifestModels(t, body)
	require.Len(t, models, 1)
	model := models[0]
	require.Equal(t, "GPT-6.1 Terra", model["display_name"])
	require.Equal(t, "low", model["default_reasoning_level"])
	require.EqualValues(t, 500_000, model["context_window"])
	require.EqualValues(t, 872_000, model["max_context_window"])
	require.Equal(t, []string{"low", "medium", "high", "xhigh", "max", "ultra"}, effortsFromManifestModel(t, model))
	require.Equal(t, []any{"text", "image"}, model["input_modalities"])
	require.Equal(t, true, model["supports_image_detail_original"])
	require.Equal(t, "code_mode_only", model["tool_mode"])
	require.Equal(t, true, model["use_responses_lite"])
	serviceTiers, ok := model["service_tiers"].([]any)
	require.True(t, ok)
	require.Len(t, serviceTiers, 1)
	tier, ok := serviceTiers[0].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "priority", tier["id"])
	require.Contains(t, model, "guardian", "new official metadata must survive the descriptor projection")
	adjusted, err := adjustAPIKeyCodexModelsManifest(body, newCodexModelsAPIKeyTestAccount(""))
	require.NoError(t, err)
	keyModel := decodeCodexManifestModels(t, adjusted)[0]
	require.Equal(t, false, keyModel["use_responses_lite"])
	require.EqualValues(t, 500_000, keyModel["context_window"])
	var normalized map[string]any
	require.NoError(t, json.Unmarshal(adjusted, &normalized))
	require.NotEmpty(t, normalized["models"])
}

func TestGPT61TerraNormalizationAndDisabledReasoning(t *testing.T) {
	for _, spelling := range []string{"gpt-6.1-terra", "openai/gpt-6.1-terra-max", "GPT_6.1_TERRA", "gpt-6.1-terra-openai-compact"} {
		require.Equal(t, "gpt-6.1-terra", normalizeKnownOpenAICodexModel(spelling))
		require.True(t, isOpenAIGPT6Model(spelling))
		require.Equal(t, "max", normalizeOpenAIReasoningEffortForModel("max", spelling))
	}
	for _, body := range []string{
		`{"reasoning":{"effort":"none"}}`,
		`{"reasoning_effort":"minimal"}`,
		`{"thinking":{"type":"disabled"}}`,
		`{"model":"gpt-6.1-terra-none"}`,
	} {
		require.Error(t, validateGPT61CompatRequest([]byte(body), "gpt-6.1-terra"))
	}
	require.NoError(t, validateGPT61CompatRequest([]byte(`{"reasoning":{"effort":"max"}}`), "gpt-6.1-terra"))
}
