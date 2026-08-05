package antigravity

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGeminiUsageMetadataOutputModalityTokens(t *testing.T) {
	usage := &GeminiUsageMetadata{
		CandidatesTokensDetails: []GeminiTokenDetail{
			{Modality: "IMAGE", TokenCount: 20},
			{Modality: "AUDIO", TokenCount: 30},
			{Modality: "IMAGE", TokenCount: 5},
			{Modality: "AUDIO", TokenCount: 4},
		},
	}

	require.Equal(t, 25, usage.ImageOutputTokens())
	require.Equal(t, 34, usage.AudioOutputTokens())
}

func TestStreamingProcessorDoneMarkerIsAnUpstreamTerminal(t *testing.T) {
	processor := NewStreamingProcessor("gemini-test")

	partial := processor.ProcessLine(`data: {"response":{"candidates":[{"content":{"parts":[{"text":"partial"}]}}]}}`)
	require.NotEmpty(t, partial)

	terminal := processor.ProcessLine("data: [DONE]")
	require.True(t, processor.MessageStopSent())
	require.Contains(t, string(terminal), `event: message_stop`)

	finalEvents, _ := processor.Finish()
	require.Empty(t, finalEvents)
}
