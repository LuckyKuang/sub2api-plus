//go:build unit

package apicompat

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

// DeepSeek's documented usage contract defines prompt_tokens = cache hit + miss:
// https://api-docs.deepseek.com/api/create-chat-completion/
// Gateway aliases must preserve the existing canonical/flat-field priority,
// including explicit zero, through both downstream protocol bridges.
func TestDeepSeekCacheUsageProtocolBridges(t *testing.T) {
	for _, tc := range []struct {
		name, extra string
		want        int
	}{
		{"hit only", "", 80},
		{"unrelated details", `,"prompt_tokens_details":{"audio_tokens":7}`, 80},
		{"nested zero", `,"prompt_tokens_details":{"cached_tokens":0}`, 0},
		{"nested wins", `,"prompt_tokens_details":{"cached_tokens":24}`, 24},
		{"flat zero", `,"cached_tokens":0`, 0},
		{"flat wins", `,"cached_tokens":32`, 32},
		{"input details zero", `,"input_tokens_details":{"cached_tokens":0}`, 0},
		{"input details wins", `,"input_tokens_details":{"cached_tokens":24},"prompt_tokens_details":{"cached_tokens":32}`, 24},
		{"cache read input zero", `,"cache_read_input_tokens":0`, 0},
		{"cache read input wins", `,"cache_read_input_tokens":24,"cache_read_tokens":32,"cached_tokens":48`, 24},
		{"cache read zero", `,"cache_read_tokens":0`, 0},
		{"cache read wins", `,"cache_read_tokens":24,"cached_tokens":32`, 24},
	} {
		t.Run(tc.name, func(t *testing.T) {
			payload := []byte(`{"prompt_tokens":100,"completion_tokens":5,"total_tokens":105,"prompt_cache_hit_tokens":80,"prompt_cache_miss_tokens":20` + tc.extra + `}`)
			var chat ChatUsage
			require.NoError(t, json.Unmarshal(payload, &chat))
			require.NotNil(t, chat.PromptTokensDetails)
			require.Equal(t, tc.want, chat.PromptTokensDetails.CachedTokens)
			if tc.name == "unrelated details" {
				require.Equal(t, 7, chat.PromptTokensDetails.AudioTokens)
			}
			messages := chatUsageToAnthropicUsage(&chat)
			require.Equal(t, 100-tc.want, messages.InputTokens)
			require.Equal(t, tc.want, messages.CacheReadInputTokens)
			require.Equal(t, 5, messages.OutputTokens)
			responses := ChatUsageToResponsesUsage(&chat)
			require.Equal(t, 100, responses.InputTokens)
			if tc.want > 0 {
				require.Equal(t, tc.want, responses.InputTokensDetails.CachedTokens)
			}
			var typed ResponsesUsage
			require.NoError(t, json.Unmarshal(payload, &typed))
			require.NotNil(t, typed.InputTokensDetails)
			require.Equal(t, tc.want, typed.InputTokensDetails.CachedTokens)
			require.Equal(t, 100, typed.InputTokens)
			require.NoError(t, json.Unmarshal([]byte(`{"prompt_tokens":12,"completion_tokens":3}`), &chat))
			require.Nil(t, chat.PromptTokensDetails, "decoding a later request must not retain cache hits")
		})
	}
	for _, field := range []string{"cache_read_input_tokens", "cache_read_tokens"} {
		var usage ResponsesUsage
		require.NoError(t, json.Unmarshal([]byte(`{"input_tokens":100,"prompt_cache_hit_tokens":80,"`+field+`":0}`), &usage))
		require.NotNil(t, usage.InputTokensDetails)
		require.Zero(t, usage.InputTokensDetails.CachedTokens)
	}
}
