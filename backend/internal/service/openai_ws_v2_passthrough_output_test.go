package service

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestOpenAIWSPassthroughStartsSemanticOutputUsesPayload(t *testing.T) {
	t.Parallel()

	require.False(t, openAIWSPassthroughStartsSemanticOutput([]byte(`{"type":"response.created","response":{"id":"resp_1"}}`)))
	require.False(t, openAIWSPassthroughStartsSemanticOutput([]byte(`{"type":"response.output_text.delta","delta":""}`)))
	require.False(t, openAIWSPassthroughStartsSemanticOutput([]byte(`{"type":"response.completed","response":{"output":[]}}`)))
	require.True(t, openAIWSPassthroughStartsSemanticOutput([]byte(`{"type":"response.output_text.delta","delta":" "}`)))
	require.True(t, openAIWSPassthroughStartsSemanticOutput([]byte(`{"type":"response.image_generation_call.partial_image","partial_image_b64":"aW1hZ2U="}`)))
}
