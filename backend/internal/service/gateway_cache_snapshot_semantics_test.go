//go:build unit || !integration

package service

import (
	"encoding/json"
	"testing"

	"github.com/LuckyKuang/sub2api-plus/internal/pkg/apicompat"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

// A compatible provider that reports the full usage object again on
// message_delta with zeroed cache fields must not erase the write already
// reported by message_start: that would silently drop the cache-write charge.
func TestMergeSSEUsagePatchKeepsReportedCacheWrites(t *testing.T) {
	usage := ClaudeUsage{
		CacheCreationInputTokens: 2048,
		CacheReadInputTokens:     4096,
	}
	mergeSSEUsagePatch(&usage, &sseUsagePatch{
		hasCacheCreationInput:    true,
		cacheCreationInputTokens: 0,
		hasCacheReadInput:        true,
		cacheReadInputTokens:     0,
	})

	require.Equal(t, 2048, usage.CacheCreationInputTokens)
	require.Equal(t, 4096, usage.CacheReadInputTokens)
}

// An explicit positive value later in the stream still replaces the earlier one.
func TestMergeSSEUsagePatchAcceptsLaterPositiveTotals(t *testing.T) {
	usage := ClaudeUsage{CacheCreationInputTokens: 100, CacheReadInputTokens: 10}
	mergeSSEUsagePatch(&usage, &sseUsagePatch{
		hasCacheCreationInput:    true,
		cacheCreationInputTokens: 250,
		hasCacheReadInput:        true,
		cacheReadInputTokens:     80,
	})

	require.Equal(t, 250, usage.CacheCreationInputTokens)
	require.Equal(t, 80, usage.CacheReadInputTokens)
}

// A breakdown-only frame derives the aggregate, and a later declared aggregate
// replaces the derived value instead of being ignored.
func TestMergeSSEUsagePatchDerivesThenAcceptsDeclaredTotal(t *testing.T) {
	usage := ClaudeUsage{}
	mergeSSEUsagePatch(&usage, &sseUsagePatch{
		hasCacheCreation5m:    true,
		cacheCreation5mTokens: 120,
		hasCacheCreation1h:    true,
		cacheCreation1hTokens: 30,
	})
	require.Equal(t, 150, usage.CacheCreationInputTokens)
	require.True(t, usage.cacheCreationInputTokensFromDetails)

	mergeSSEUsagePatch(&usage, &sseUsagePatch{
		hasCacheCreationInput:    true,
		cacheCreationInputTokens: 150,
	})
	require.Equal(t, 150, usage.CacheCreationInputTokens)
	require.False(t, usage.cacheCreationInputTokensFromDetails)
}

// A declared aggregate survives a later frame whose breakdown reports zeros:
// stale details must not repricing the write as a 5m class or drop it.
func TestMergeSSEUsagePatchIgnoresStaleZeroBreakdown(t *testing.T) {
	usage := ClaudeUsage{}
	mergeSSEUsagePatch(&usage, &sseUsagePatch{
		hasCacheCreationInput:    true,
		cacheCreationInputTokens: 900,
	})
	mergeSSEUsagePatch(&usage, &sseUsagePatch{
		hasCacheCreation5m:    true,
		cacheCreation5mTokens: 0,
		hasCacheCreation1h:    true,
		cacheCreation1hTokens: 0,
	})

	require.Equal(t, 900, usage.CacheCreationInputTokens)
	require.Equal(t, 900, usage.cacheCreationTotal())
}

func TestClaudeUsageCacheCreationTotal(t *testing.T) {
	tests := []struct {
		name  string
		usage ClaudeUsage
		want  int
	}{
		{"aggregate_wins", ClaudeUsage{CacheCreationInputTokens: 500, CacheCreation5mTokens: 10, CacheCreation1hTokens: 5}, 500},
		{"details_only", ClaudeUsage{CacheCreation5mTokens: 10, CacheCreation1hTokens: 5}, 15},
		{"negative_details_clamp", ClaudeUsage{CacheCreation5mTokens: -3, CacheCreation1hTokens: 5}, 5},
		{"all_zero", ClaudeUsage{}, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, tt.usage.cacheCreationTotal())
		})
	}
}

func TestOpenAIUsageCacheCreationTotal(t *testing.T) {
	tests := []struct {
		name  string
		usage OpenAIUsage
		want  int
	}{
		{"aggregate_wins", OpenAIUsage{CacheCreationInputTokens: 300, CacheCreation30mInputTokens: 100}, 300},
		{"ttl_only", OpenAIUsage{CacheCreation30mInputTokens: 100, CacheCreation12hInputTokens: 20}, 120},
		{"all_zero", OpenAIUsage{}, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, tt.usage.cacheCreationTotal())
		})
	}
}

// With only an aggregate, the override materializes the class it is moving the
// write into; the total never comes from a stale zero detail sum.
func TestApplyCacheTTLOverrideUsesAggregateAsTotal(t *testing.T) {
	usage := ClaudeUsage{CacheCreationInputTokens: 700}
	require.True(t, applyCacheTTLOverride(&usage, "5m"))
	require.Equal(t, 700, usage.CacheCreation5mTokens)
	require.Equal(t, 0, usage.CacheCreation1hTokens)

	require.False(t, applyCacheTTLOverride(&usage, "5m"))

	require.True(t, applyCacheTTLOverride(&usage, "1h"))
	require.Equal(t, 700, usage.CacheCreation1hTokens)
	require.Equal(t, 0, usage.CacheCreation5mTokens)
}

// A contradictory detail sum is capped by the authoritative aggregate so the
// response never claims fewer writes than billing charges.
func TestApplyCacheTTLOverridePrefersAggregateOverDetailSum(t *testing.T) {
	usage := ClaudeUsage{
		CacheCreationInputTokens: 1000,
		CacheCreation5mTokens:    100,
		CacheCreation1hTokens:    50,
	}
	require.True(t, applyCacheTTLOverride(&usage, "1h"))
	require.Equal(t, 1000, usage.CacheCreation1hTokens)
	require.Equal(t, 0, usage.CacheCreation5mTokens)
}

func TestApplyCacheTTLOverrideNoWrites(t *testing.T) {
	usage := ClaudeUsage{}
	require.False(t, applyCacheTTLOverride(&usage, "1h"))
	require.Equal(t, ClaudeUsage{}, usage)
}

// The JSON rewrite must not invent a cache_creation object the upstream never
// sent, and must move the same total the in-memory usage moved.
func TestRewriteCacheCreationJSON(t *testing.T) {
	t.Run("no_object_is_untouched", func(t *testing.T) {
		usageObj := map[string]any{"cache_creation_input_tokens": float64(400)}
		require.False(t, rewriteCacheCreationJSON(usageObj, "1h"))
		_, exists := usageObj["cache_creation"]
		require.False(t, exists)
	})

	t.Run("moves_aggregate_into_target_class", func(t *testing.T) {
		usageObj := map[string]any{
			"cache_creation_input_tokens": float64(400),
			"cache_creation": map[string]any{
				"ephemeral_5m_input_tokens": float64(0),
				"ephemeral_1h_input_tokens": float64(0),
			},
		}
		require.True(t, rewriteCacheCreationJSON(usageObj, "1h"))
		cc, ok := usageObj["cache_creation"].(map[string]any)
		require.True(t, ok, "cache_creation must stay a JSON object")
		require.Equal(t, float64(400), cc["ephemeral_1h_input_tokens"])
		require.Equal(t, float64(0), cc["ephemeral_5m_input_tokens"])
	})

	t.Run("already_target_class", func(t *testing.T) {
		usageObj := map[string]any{
			"cache_creation_input_tokens": float64(12),
			"cache_creation": map[string]any{
				"ephemeral_5m_input_tokens": float64(12),
				"ephemeral_1h_input_tokens": float64(0),
			},
		}
		require.False(t, rewriteCacheCreationJSON(usageObj, "5m"))
	})
}

// A later merge that only repeats zero cache buckets must keep the earlier ones.
func TestMergeAnthropicUsageKeepsEarlierCacheBuckets(t *testing.T) {
	dst := &ClaudeUsage{
		InputTokens:              10,
		CacheReadInputTokens:     800,
		CacheCreationInputTokens: 200,
	}
	mergeAnthropicUsage(dst, apicompat.AnthropicUsage{
		InputTokens:          10,
		CacheReadInputTokens: 0,
	})
	require.Equal(t, 800, dst.CacheReadInputTokens)
	require.Equal(t, 200, dst.CacheCreationInputTokens)

	mergeAnthropicUsage(dst, apicompat.AnthropicUsage{
		InputTokens:              10,
		CacheReadInputTokens:     900,
		CacheCreationInputTokens: 250,
	})
	require.Equal(t, 900, dst.CacheReadInputTokens)
	require.Equal(t, 250, dst.CacheCreationInputTokens)
}

// A breakdown-only snapshot fills the aggregate from the classes.
func TestMergeAnthropicUsageDerivesTotalFromBreakdown(t *testing.T) {
	fiveMinutes := 70
	oneHour := 30
	dst := &ClaudeUsage{}
	mergeAnthropicUsage(dst, apicompat.AnthropicUsage{
		CacheCreation: &apicompat.AnthropicCacheCreation{
			Ephemeral5mInputTokens: &fiveMinutes,
			Ephemeral1hInputTokens: &oneHour,
		},
	})
	require.Equal(t, 100, dst.CacheCreationInputTokens)
	require.Equal(t, 70, dst.CacheCreation5mTokens)
	require.Equal(t, 30, dst.CacheCreation1hTokens)

	// A declared aggregate replaces the derived one.
	mergeAnthropicUsage(dst, apicompat.AnthropicUsage{CacheCreationInputTokens: 120})
	require.Equal(t, 120, dst.CacheCreationInputTokens)
}

// The compatible-prompt normalization subtracts the same authoritative total
// and never lets a zero cache field erase a reported one.
func TestNormalizeAnthropicCompatiblePromptUsageKeepsCacheBuckets(t *testing.T) {
	usage := &ClaudeUsage{CacheReadInputTokens: 500, CacheCreationInputTokens: 100}
	usageNode := gjson.Parse(`{"prompt_tokens":900,"cache_read_input_tokens":0,"cache_creation_input_tokens":0}`)
	mergeAnthropicUsageJSON(usageNode, usage)
	require.Equal(t, 500, usage.CacheReadInputTokens)
	require.Equal(t, 100, usage.CacheCreationInputTokens)
	require.Equal(t, 300, usage.InputTokens)
}

func TestAnthropicSSEUsageRetainsDeclaredTotalAndUpdatesDerivedTotal(t *testing.T) {
	frames := []string{
		`{"type":"message_start","message":{"usage":{"input_tokens":15000,"output_tokens":22,"prompt_tokens":15000,"cache_creation":{"ephemeral_5m_input_tokens":100}}}}`,
		`{"type":"message_delta","usage":{"cache_creation_input_tokens":0,"output_tokens":0,"cache_creation":{"ephemeral_1h_input_tokens":200}}}`,
		`{"type":"message_delta","usage":{"cache_creation_input_tokens":11000}}`,
		`{"type":"message_delta","usage":{"cache_creation_input_tokens":0,"cache_creation":{"ephemeral_1h_input_tokens":3000}}}`,
	}
	gateway := &GatewayService{}
	for name, merge := range map[string]func(*testing.T, string, *ClaudeUsage){
		"passthrough": func(_ *testing.T, payload string, usage *ClaudeUsage) {
			parseSSEUsagePassthrough(payload, usage)
		},
		"gateway": func(_ *testing.T, payload string, usage *ClaudeUsage) {
			gateway.parseSSEUsage(payload, usage)
		},
		"typed bridge": func(t *testing.T, payload string, usage *ClaudeUsage) {
			var event apicompat.AnthropicStreamEvent
			require.NoError(t, json.Unmarshal([]byte(payload), &event))
			if event.Message != nil {
				mergeAnthropicUsage(usage, event.Message.Usage)
			}
			if event.Usage != nil {
				mergeAnthropicUsage(usage, *event.Usage)
			}
		},
	} {
		t.Run(name, func(t *testing.T) {
			var usage ClaudeUsage
			for i, frame := range frames {
				merge(t, frame, &usage)
				wantCreation := []int{100, 300, 11000, 11000}[i]
				require.Equal(t, wantCreation, usage.cacheCreationTotal(), "frame %d", i)
				require.Equal(t, 15000-wantCreation, usage.InputTokens, "frame %d", i)
				require.Equal(t, 22, usage.OutputTokens, "frame %d", i)
			}
			require.Equal(t, 100, usage.CacheCreation5mTokens)
			require.Equal(t, 3000, usage.CacheCreation1hTokens)
		})
	}
}
