//go:build unit

package apicompat

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestChatUsageStepFunCacheAlias(t *testing.T) {
	for _, tc := range []struct {
		name, details string
		want          int
	}{
		{"flat", "", 512},
		{"other details", `,"prompt_tokens_details":{"audio_tokens":7}`, 512},
		{"nested zero", `,"prompt_tokens_details":{"cached_tokens":0,"audio_tokens":7}`, 0},
		{"nested wins", `,"prompt_tokens_details":{"cached_tokens":256,"audio_tokens":7}`, 256},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var usage ChatUsage
			require.NoError(t, json.Unmarshal([]byte(`{"prompt_tokens":591,"completion_tokens":120,"total_tokens":711,"cached_tokens":512`+tc.details+`}`), &usage))
			require.Equal(t, tc.want, usage.PromptTokensDetails.CachedTokens)
			if tc.details != "" {
				require.Equal(t, 7, usage.PromptTokensDetails.AudioTokens)
			}
			responses := ChatUsageToResponsesUsage(&usage)
			require.Equal(t, 591, responses.InputTokens)
			if tc.want > 0 {
				require.Equal(t, tc.want, responses.InputTokensDetails.CachedTokens)
			}
			messages := chatUsageToAnthropicUsage(&usage)
			require.Equal(t, 591-tc.want, messages.InputTokens)
			require.Equal(t, tc.want, messages.CacheReadInputTokens)
			require.Equal(t, 120, messages.OutputTokens)
			// Reusing a decode target must not retain the preceding cached count.
			require.NoError(t, json.Unmarshal([]byte(`{"prompt_tokens":12,"completion_tokens":3}`), &usage))
			require.Nil(t, usage.PromptTokensDetails)
		})
	}
	var usage ChatUsage
	require.NoError(t, json.Unmarshal([]byte(`{"prompt_tokens":12,"cached_tokens":-1}`), &usage))
	require.Zero(t, usage.PromptTokensDetails.CachedTokens)
}
