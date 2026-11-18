//go:build unit || !integration

package service

import (
	"testing"

	"github.com/LuckyKuang/sub2api-plus/internal/pkg/apicompat"
	"github.com/stretchr/testify/require"
)

// Anthropic's cache_creation breakdown decides the TTL class a write is billed
// at. The flat total must follow the breakdown sum, and the uncached input bucket
// must be derived from that same sum, or the total prompt count drifts.
func TestMergeAnthropicUsageKeepsCacheCreationTTLClasses(t *testing.T) {
	fiveMinutes := 2
	oneHour := 3
	dst := &ClaudeUsage{}
	mergeAnthropicUsage(dst, apicompat.AnthropicUsage{
		InputTokens:              12,
		OutputTokens:             7,
		CacheReadInputTokens:     9,
		CacheCreationInputTokens: 5,
		CacheCreation: &apicompat.AnthropicCacheCreation{
			Ephemeral5mInputTokens: &fiveMinutes,
			Ephemeral1hInputTokens: &oneHour,
		},
	})

	require.Equal(t, 2, dst.CacheCreation5mTokens)
	require.Equal(t, 3, dst.CacheCreation1hTokens)
	require.Equal(t, 5, dst.CacheCreationInputTokens)
	require.Equal(t, 12, dst.InputTokens)
	require.Equal(t, 9, dst.CacheReadInputTokens)
	require.Equal(t, 7, dst.OutputTokens)
}

// A breakdown whose flat field is zero still carries the writes.
func TestMergeAnthropicUsageUsesBreakdownWhenFlatFieldIsZero(t *testing.T) {
	fiveMinutes := 4
	zero := 0
	dst := &ClaudeUsage{}
	mergeAnthropicUsage(dst, apicompat.AnthropicUsage{
		InputTokens: 10,
		CacheCreation: &apicompat.AnthropicCacheCreation{
			Ephemeral5mInputTokens: &fiveMinutes,
			Ephemeral1hInputTokens: &zero,
		},
	})

	require.Equal(t, 4, dst.CacheCreation5mTokens)
	require.Zero(t, dst.CacheCreation1hTokens)
	require.Equal(t, 4, dst.CacheCreationInputTokens)
}

// Without a breakdown the flat field keeps its previous precedence and no TTL
// class is invented.
func TestMergeAnthropicUsageWithoutBreakdownLeavesTTLClassesEmpty(t *testing.T) {
	dst := &ClaudeUsage{}
	mergeAnthropicUsage(dst, apicompat.AnthropicUsage{
		InputTokens:              10,
		OutputTokens:             2,
		CacheCreationInputTokens: 6,
	})

	require.Zero(t, dst.CacheCreation5mTokens)
	require.Zero(t, dst.CacheCreation1hTokens)
	require.Equal(t, 6, dst.CacheCreationInputTokens)
	require.Equal(t, 10, dst.InputTokens)
}

// An OpenAI-style authoritative prompt total subtracts the breakdown sum, not the
// flat field, so the uncached bucket is never inflated.
func TestMergeAnthropicUsageReconcilesPromptTotalWithBreakdown(t *testing.T) {
	fiveMinutes := 2
	oneHour := 3
	miss := 12
	dst := &ClaudeUsage{}
	mergeAnthropicUsage(dst, apicompat.AnthropicUsage{
		PromptTokens:          20,
		PromptCacheMissTokens: &miss,
		CacheReadInputTokens:  9,
		CacheCreation: &apicompat.AnthropicCacheCreation{
			Ephemeral5mInputTokens: &fiveMinutes,
			Ephemeral1hInputTokens: &oneHour,
		},
	})

	require.Equal(t, 12, dst.InputTokens)
	require.Equal(t, 9, dst.CacheReadInputTokens)
	require.Equal(t, 5, dst.CacheCreationInputTokens)
}

func TestMergeAnthropicUsageDerivesUncachedBucketFromBreakdown(t *testing.T) {
	fiveMinutes := 2
	oneHour := 3
	dst := &ClaudeUsage{}
	mergeAnthropicUsage(dst, apicompat.AnthropicUsage{
		PromptTokens:         20,
		CacheReadInputTokens: 9,
		CacheCreation: &apicompat.AnthropicCacheCreation{
			Ephemeral5mInputTokens: &fiveMinutes,
			Ephemeral1hInputTokens: &oneHour,
		},
	})

	require.Equal(t, 6, dst.InputTokens, "20 prompt - 9 read - 5 write = 6 uncached")
	require.Equal(t, 5, dst.CacheCreationInputTokens)
}
