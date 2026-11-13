//go:build unit || !integration

package apicompat

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

// OPENAI_RESPONSES.md requires typed compatibility and reserialization to
// preserve the reported TTL details; malformed details must not reject usage.
func TestResponsesCacheWriteTTLTypedRoundTrip(t *testing.T) {
	for _, tc := range []struct {
		name, details string
		short, long   int
	}{
		{"mixed", `{"ttl_30m_tokens":4,"ttl_12h_tokens":6}`, 4, 6},
		{"invalid", `{"ttl_30m_tokens":"0","ttl_12h_tokens":10}`, 0, 0},
		{"partial", `{"ttl_12h_tokens":10}`, 0, 0},
	} {
		for _, field := range []string{"input_tokens_details", "prompt_tokens_details"} {
			t.Run(tc.name+"/"+field, func(t *testing.T) {
				var usage ResponsesUsage
				require.NoError(t, json.Unmarshal([]byte(`{"input_tokens":10,"output_tokens":0,"`+field+`":{"cache_write_tokens":10,"cache_write_tokens_details":`+tc.details+`}}`), &usage))
				require.Equal(t, 10, usage.CacheCreationInputTokens)
				for _, candidate := range []*ResponsesUsage{&usage, ChatUsageToResponsesUsage(chatUsageFromResponsesUsage(&usage))} {
					short, long := candidate.CacheWriteTTLTokens()
					require.Equal(t, tc.short, short)
					require.Equal(t, tc.long, long)
					encoded, err := json.Marshal(candidate)
					require.NoError(t, err)
					require.JSONEq(t, tc.details, gjson.GetBytes(encoded, "input_tokens_details.cache_write_tokens_details").Raw)
					var decoded ResponsesUsage
					require.NoError(t, json.Unmarshal(encoded, &decoded))
					short, long = decoded.CacheWriteTTLTokens()
					require.Equal(t, tc.short, short)
					require.Equal(t, tc.long, long)
				}
			})
		}
	}
}
