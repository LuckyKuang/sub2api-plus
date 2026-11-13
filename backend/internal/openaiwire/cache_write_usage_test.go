//go:build unit || !integration

package openaiwire

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

// CHANNEL_PRICING.md: reported integer buckets must exhaust the aggregate;
// incomplete/invalid observations cannot infer a duration from request intent.
func TestCacheWriteTTLValidation(t *testing.T) {
	for _, tc := range []struct {
		name, details          string
		aggregate, short, long int
	}{
		{"30m", `{"ttl_30m_tokens":10,"ttl_12h_tokens":0}`, 10, 10, 0},
		{"12h", `{"ttl_30m_tokens":0,"ttl_12h_tokens":10}`, 10, 0, 10},
		{"mixed", `{"ttl_30m_tokens":4,"ttl_12h_tokens":6}`, 10, 4, 6},
		{"zero", `{"ttl_30m_tokens":0,"ttl_12h_tokens":0}`, 0, 0, 0},
		{"absent", ``, 10, 0, 0},
		{"null", `null`, 10, 0, 0},
		{"partial", `{"ttl_12h_tokens":10}`, 10, 0, 0},
		{"short sum", `{"ttl_30m_tokens":0,"ttl_12h_tokens":9}`, 10, 0, 0},
		{"over sum", `{"ttl_30m_tokens":1,"ttl_12h_tokens":10}`, 10, 0, 0},
		{"negative", `{"ttl_30m_tokens":-1,"ttl_12h_tokens":11}`, 10, 0, 0},
		{"fraction", `{"ttl_30m_tokens":0.5,"ttl_12h_tokens":9.5}`, 10, 0, 0},
		{"strings", `{"ttl_30m_tokens":"0","ttl_12h_tokens":"10"}`, 10, 0, 0},
		{"overflow", `{"ttl_30m_tokens":18446744073709551616,"ttl_12h_tokens":0}`, 10, 0, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			short, long := CacheWriteTTLTokens(gjson.Parse(tc.details), tc.aggregate)
			require.Equal(t, tc.short, short)
			require.Equal(t, tc.long, long)
		})
	}
}

func TestCacheWriteTTLDetailsAliasPriority(t *testing.T) {
	for _, tc := range []struct {
		name, input string
		short, long int
	}{
		{"prompt fallback", `{"input_tokens_details":{"cached_tokens":1},"prompt_tokens_details":{"cache_write_tokens_details":{"ttl_30m_tokens":4,"ttl_12h_tokens":6}}}`, 4, 6},
		{"canonical wins", `{"input_tokens_details":{"cache_write_tokens_details":{"ttl_30m_tokens":10,"ttl_12h_tokens":0}},"prompt_tokens_details":{"cache_write_tokens_details":{"ttl_30m_tokens":0,"ttl_12h_tokens":10}}}`, 10, 0},
		{"invalid canonical stops fallback", `{"input_tokens_details":{"cache_write_tokens_details":null},"prompt_tokens_details":{"cache_write_tokens_details":{"ttl_30m_tokens":0,"ttl_12h_tokens":10}}}`, 0, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			short, long := CacheWriteTTLTokens(CacheWriteTokenDetails(gjson.Parse(tc.input)), 10)
			require.Equal(t, tc.short, short)
			require.Equal(t, tc.long, long)
		})
	}
}
