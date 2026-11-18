//go:build unit || !integration

package service

import (
	"testing"

	"github.com/LuckyKuang/sub2api-plus/internal/pkg/apicompat"
	"github.com/stretchr/testify/require"
)

func TestNormalizeAnthropicPromptTotal(t *testing.T) {
	t.Run("miss_bucket_is_authoritative", func(t *testing.T) {
		miss := 42
		usage := &ClaudeUsage{promptTotalTokens: 100, promptTotalPresent: true}
		normalizeAnthropicPromptTotal(usage, 100, &miss)
		require.Equal(t, 42, usage.InputTokens)
		require.False(t, usage.promptTotalPresent, "miss 桶与总量互斥，记住的总量必须失效")
	})

	t.Run("total_recomputes_uncached_bucket", func(t *testing.T) {
		usage := &ClaudeUsage{
			CacheReadInputTokens:     120,
			CacheCreationInputTokens: 80,
		}
		normalizeAnthropicPromptTotal(usage, 1000, nil)
		require.Equal(t, 800, usage.InputTokens)
		require.Equal(t, 1000, usage.promptTotalTokens)
		require.True(t, usage.promptTotalPresent)
	})

	t.Run("cache_only_update_reuses_remembered_total", func(t *testing.T) {
		usage := &ClaudeUsage{
			CacheReadInputTokens:     120,
			CacheCreationInputTokens: 80,
		}
		normalizeAnthropicPromptTotal(usage, 1000, nil)

		// Later frame refines only the cache bucket, no prompt total.
		usage.CacheReadInputTokens = 700
		normalizeAnthropicPromptTotal(usage, 0, nil)
		require.Equal(t, 220, usage.InputTokens, "只更新缓存的帧仍要用记住的总量重算")
	})

	t.Run("no_total_leaves_input_untouched", func(t *testing.T) {
		usage := &ClaudeUsage{InputTokens: 17, CacheReadInputTokens: 5}
		normalizeAnthropicPromptTotal(usage, 0, nil)
		require.Equal(t, 17, usage.InputTokens)
	})
}

// A stream that reports prompt_tokens once and then only refines cache buckets
// must keep `input + cache_read + cache_write == prompt_tokens`.
func TestMergeSSEUsagePatchReconcilesCacheOnlyUpdates(t *testing.T) {
	usage := &ClaudeUsage{}
	mergeSSEUsagePatch(usage, &sseUsagePatch{
		promptTokens:             1000,
		inputTokens:              1000,
		hasInputTokens:           true,
		cacheReadInputTokens:     100,
		hasCacheReadInput:        true,
		cacheCreationInputTokens: 200,
		hasCacheCreationInput:    true,
	})
	require.Equal(t, 700, usage.InputTokens)

	mergeSSEUsagePatch(usage, &sseUsagePatch{
		cacheReadInputTokens: 400,
		hasCacheReadInput:    true,
	})
	require.Equal(t, 400, usage.CacheReadInputTokens)
	require.Equal(t, 400, usage.InputTokens, "缓存只增不减时未缓存输入必须同步减少")
	require.Equal(t, 1000, usage.InputTokens+usage.CacheReadInputTokens+usage.cacheCreationTotal(), "三个互斥桶之和必须回到 prompt 总量")
}

func TestMergeSSEUsagePatchKeepsPromptTotalAcrossFrames(t *testing.T) {
	usage := &ClaudeUsage{}
	mergeSSEUsagePatch(usage, &sseUsagePatch{promptTokens: 500, inputTokens: 500, hasInputTokens: true})
	require.Equal(t, 500, usage.InputTokens)

	// A frame without prompt_tokens but with a cache miss bucket takes over.
	miss := 120
	mergeSSEUsagePatch(usage, &sseUsagePatch{promptCacheMissTokens: &miss})
	require.Equal(t, 120, usage.InputTokens)
	require.False(t, usage.promptTotalPresent)
}

// The Anthropic usage merge uses the same reconciliation, so a cache-only
// snapshot after a prompt total still lands in the right bucket.
func TestMergeAnthropicUsageReconcilesCacheOnlySnapshot(t *testing.T) {
	dst := &ClaudeUsage{}
	mergeAnthropicUsage(dst, apicompat.AnthropicUsage{
		PromptTokens:             900,
		InputTokens:              900,
		CacheReadInputTokens:     100,
		CacheCreationInputTokens: 50,
	})
	require.Equal(t, 750, dst.InputTokens)

	mergeAnthropicUsage(dst, apicompat.AnthropicUsage{
		CacheReadInputTokens: 300,
	})
	require.Equal(t, 300, dst.CacheReadInputTokens)
	require.Equal(t, 550, dst.InputTokens)
}

// Kimi-style passthrough: message_start reports total input, message_delta
// reports cache buckets only. Both frames must land on one bucket split.
func TestParseSSEUsagePassthroughReconcilesPromptTotal(t *testing.T) {
	usage := &ClaudeUsage{}
	parseSSEUsagePassthrough(`{"type":"message_start","message":{"usage":{"input_tokens":100,"prompt_tokens":100,"cache_read_input_tokens":20,"cache_creation_input_tokens":30}}}`, usage)
	require.Equal(t, 50, usage.InputTokens)
	require.Equal(t, 20, usage.CacheReadInputTokens)
	require.Equal(t, 30, usage.CacheCreationInputTokens)

	parseSSEUsagePassthrough(`{"type":"message_delta","usage":{"prompt_tokens":100,"cache_read_input_tokens":60}}`, usage)
	require.Equal(t, 60, usage.CacheReadInputTokens)
	require.Equal(t, 10, usage.InputTokens)

	// A later frame without prompt_tokens still reconciles from the remembered total.
	parseSSEUsagePassthrough(`{"type":"message_delta","usage":{"cache_read_input_tokens":70}}`, usage)
	require.Equal(t, 70, usage.CacheReadInputTokens)
	require.Equal(t, 0, usage.InputTokens)
}
