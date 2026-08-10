//go:build unit

package service

import (
	"testing"

	"github.com/LuckyKuang/sub2api-plus/internal/pkg/apicompat"
	"github.com/stretchr/testify/require"
)

func TestCopyOpenAIUsageFromResponsesUsageTrustsCanonicalCacheCreationValue(t *testing.T) {
	usage := &apicompat.ResponsesUsage{
		InputTokens:              20,
		OutputTokens:             2,
		CacheCreationInputTokens: 0,
		InputTokensDetails: &apicompat.ResponsesInputTokensDetails{
			CachedTokens:     3,
			CacheWriteTokens: 19,
		},
		OutputTokensDetails: &apicompat.ResponsesOutputTokensDetails{
			AudioTokens: 1,
		},
	}

	got := copyOpenAIUsageFromResponsesUsage(usage)

	require.Equal(t, 20, got.InputTokens)
	require.Equal(t, 3, got.CacheReadInputTokens)
	require.Zero(t, got.CacheCreationInputTokens)
	require.Equal(t, 1, got.AudioOutputTokens)
}

func TestAddOpenAIUsagePreservesEveryTokenClass(t *testing.T) {
	dst := OpenAIUsage{
		InputTokens:              1,
		ImageInputTokens:         2,
		OutputTokens:             3,
		CacheCreationInputTokens: 4,
		CacheReadInputTokens:     5,
		ImageOutputTokens:        6,
		AudioOutputTokens:        7,
	}
	addOpenAIUsage(&dst, OpenAIUsage{
		InputTokens:              10,
		ImageInputTokens:         20,
		OutputTokens:             30,
		CacheCreationInputTokens: 40,
		CacheReadInputTokens:     50,
		ImageOutputTokens:        60,
		AudioOutputTokens:        70,
	})

	require.Equal(t, OpenAIUsage{
		InputTokens:              11,
		ImageInputTokens:         22,
		OutputTokens:             33,
		CacheCreationInputTokens: 44,
		CacheReadInputTokens:     55,
		ImageOutputTokens:        66,
		AudioOutputTokens:        77,
	}, dst)
}
