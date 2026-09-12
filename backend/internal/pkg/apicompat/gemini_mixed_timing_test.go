package apicompat

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestObserveGeminiOutputRetainsTokensAfterNonTokenParts(t *testing.T) {
	for _, tc := range []struct {
		name, first string
		kind        StreamOutputKind
	}{
		{"image", `{"inlineData":{"mimeType":"image/png","data":"image"}}`, StreamOutputImage},
		{"audio", `{"inline_data":{"mime_type":"audio/wav","data":"audio"}}`, StreamOutputAudio},
		{"signature", `{"thoughtSignature":"signature"}`, StreamOutputReasoning},
		{"execution result", `{"codeExecutionResult":{"output":"result"}}`, StreamOutputTool},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, tail := range []string{`{"text":"answer"}`, `{"thought":true,"text":"reasoning"}`, `{"functionCall":{"name":"search","args":{}}}`} {
				observation := ObserveGeminiOutput([]byte(`{"candidates":[{"content":{"parts":[` + tc.first + `,` + tail + `]}}]}`))
				require.Equal(t, tc.kind, observation.Kind)
				require.True(t, observation.MeaningfulOutput)
				require.True(t, observation.TokenLikeDelta)
			}
		})
	}
	observation := ObserveGeminiOutput([]byte(`{"response":{"candidates":[{"content":{"parts":[{"thoughtSignature":"signature"}]}},{"content":{"parts":[{"text":"answer"}]}}]}}`))
	require.True(t, observation.TokenLikeDelta)
	require.Equal(t, StreamOutputReasoning, observation.Kind)
}
