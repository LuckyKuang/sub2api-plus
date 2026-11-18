//go:build unit || !integration

package apicompat

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// cache_write_tokens and cache_creation_tokens are two spellings of one
// quantity. A declared write wins even when it is zero or negative, otherwise the
// same tokens would be counted twice while the provider says there were none.
func TestChatCacheWritePresenceAcrossAnthropicConverters(t *testing.T) {
	for _, test := range []struct {
		name     string
		details  string
		creation int
	}{
		{"missing write falls back", `"cache_creation_tokens":4`, 4},
		{"zero write is authoritative", `"cache_write_tokens":0,"cache_creation_tokens":4`, 0},
		{"null write is authoritative", `"cache_write_tokens":null,"cache_creation_tokens":4`, 0},
		{"negative write clamps", `"cache_write_tokens":-2,"cache_creation_tokens":4`, 0},
		{"positive write wins", `"cache_write_tokens":2,"cache_creation_tokens":4`, 2},
	} {
		t.Run(test.name, func(t *testing.T) {
			var response ChatCompletionsResponse
			payload := `{"id":"chat_cache","choices":[{"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":10,"completion_tokens":2,"prompt_tokens_details":{"cached_tokens":3,` + test.details + `}}}`
			require.NoError(t, json.Unmarshal([]byte(payload), &response))

			// Presence survives a JSON round trip: an explicit zero stays on the wire.
			encoded, err := json.Marshal(response)
			require.NoError(t, err)
			if strings.Contains(test.details, `"cache_write_tokens":0`) {
				require.Contains(t, string(encoded), `"cache_write_tokens":0`)
			}
			var roundTripped ChatCompletionsResponse
			require.NoError(t, json.Unmarshal(encoded, &roundTripped))

			assertUsage := func(usage AnthropicUsage) {
				require.Equal(t, 7-test.creation, usage.InputTokens)
				require.Equal(t, 2, usage.OutputTokens)
				require.Equal(t, 3, usage.CacheReadInputTokens)
				require.Equal(t, test.creation, usage.CacheCreationInputTokens)
				require.Nil(t, usage.CacheCreation)
			}
			assertUsage(ChatCompletionsResponseToAnthropic(&response, "claude-sonnet-4").Usage)
			assertUsage(ChatCompletionsResponseToAnthropic(&roundTripped, "claude-sonnet-4").Usage)

			converted := ChatCompletionsResponseToResponses(&response, "claude-sonnet-4", nil, nil, false, nil)
			require.NotNil(t, converted.Usage)
			require.Equal(t, test.creation, converted.Usage.CacheCreationInputTokens)
			encodedResponses, err := json.Marshal(converted)
			require.NoError(t, err)
			var decoded ResponsesResponse
			require.NoError(t, json.Unmarshal(encodedResponses, &decoded))
			assertUsage(ResponsesToAnthropic(&decoded, "claude-sonnet-4").Usage)
		})
	}
}

// A resolved zero must not leave an uncounted amount in the detail aliases.
func TestChatUsageToResponsesUsage_ZeroWriteClearsAliasDetails(t *testing.T) {
	usage := &ChatUsage{
		PromptTokens:     10,
		CompletionTokens: 2,
		PromptTokensDetails: &ChatTokenDetails{
			CachedTokens:            3,
			CacheCreationTokens:     4,
			CacheWriteTokens:        0,
			cacheWriteTokensPresent: true,
		},
	}

	out := ChatUsageToResponsesUsage(usage)
	require.NotNil(t, out)
	require.Zero(t, out.CacheCreationInputTokens)
	require.NotNil(t, out.InputTokensDetails)
	require.Zero(t, out.InputTokensDetails.CacheCreationTokens)
	require.Zero(t, out.InputTokensDetails.CacheWriteTokens)
}

func TestChatTokenDetailsCacheCreationInputTokens(t *testing.T) {
	zero := &ChatTokenDetails{CacheCreationTokens: 4, cacheWriteTokensPresent: true}
	require.Zero(t, zero.cacheCreationInputTokens())

	negative := &ChatTokenDetails{CacheCreationTokens: 4, CacheWriteTokens: -2, cacheWriteTokensPresent: true}
	require.Zero(t, negative.cacheCreationInputTokens())

	fallback := &ChatTokenDetails{CacheCreationTokens: 4}
	require.Equal(t, 4, fallback.cacheCreationInputTokens())

	positive := &ChatTokenDetails{CacheCreationTokens: 4, CacheWriteTokens: 2}
	require.Equal(t, 2, positive.cacheCreationInputTokens())
}
