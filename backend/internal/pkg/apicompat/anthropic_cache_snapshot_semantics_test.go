//go:build unit || !integration

package apicompat

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Compatible providers repeat the whole usage object on message_delta with
// zeroed fields. Those zeros must not erase values the provider already sent.
func TestAnthropicStreamUsageIgnoresRepeatedZeros(t *testing.T) {
	state := NewAnthropicEventToResponsesState()

	start := AnthropicStreamEvent{
		Type: "message_start",
		Message: &AnthropicResponse{
			ID:    "msg_zero",
			Model: "claude-sonnet-4",
			Usage: AnthropicUsage{InputTokens: 12, OutputTokens: 5, CacheReadInputTokens: 400, CacheCreationInputTokens: 90},
		},
	}
	AnthropicEventToResponsesEvents(&start, state)

	delta := AnthropicStreamEvent{
		Type:  "message_delta",
		Usage: &AnthropicUsage{OutputTokens: 0, CacheReadInputTokens: 0, CacheCreationInputTokens: 0},
	}
	AnthropicEventToResponsesEvents(&delta, state)

	assert.Equal(t, 5, state.OutputTokens)
	assert.Equal(t, 400, state.CacheReadInputTokens)
	assert.Equal(t, 90, state.CacheCreationInputTokens)
}

// A stale zero breakdown must not repricing a declared positive aggregate down
// to zero: the aggregate is the provider's own total.
func TestAnthropicStreamUsageKeepsDeclaredTotalAgainstZeroBreakdown(t *testing.T) {
	zero := 0
	state := NewAnthropicEventToResponsesState()

	start := AnthropicStreamEvent{
		Type: "message_start",
		Message: &AnthropicResponse{
			ID:    "msg_declared",
			Model: "claude-sonnet-4",
			Usage: AnthropicUsage{
				InputTokens:              12,
				CacheCreationInputTokens: 600,
				CacheCreation:            &AnthropicCacheCreation{Ephemeral5mInputTokens: &zero},
			},
		},
	}
	AnthropicEventToResponsesEvents(&start, state)

	assert.Equal(t, 600, state.CacheCreationInputTokens)

	delta := AnthropicStreamEvent{
		Type:  "message_delta",
		Usage: &AnthropicUsage{CacheCreation: &AnthropicCacheCreation{Ephemeral1hInputTokens: &zero}},
	}
	AnthropicEventToResponsesEvents(&delta, state)

	assert.Equal(t, 600, state.CacheCreationInputTokens)
}

// With no aggregate at all, the breakdown is the only source and keeps
// accumulating as later frames refine it.
func TestAnthropicStreamUsageDerivesTotalFromBreakdown(t *testing.T) {
	fiveMinutes := 40
	oneHour := 10
	state := NewAnthropicEventToResponsesState()

	start := AnthropicStreamEvent{
		Type: "message_start",
		Message: &AnthropicResponse{
			ID:    "msg_breakdown",
			Model: "claude-sonnet-4",
			Usage: AnthropicUsage{
				InputTokens:   8,
				CacheCreation: &AnthropicCacheCreation{Ephemeral5mInputTokens: &fiveMinutes},
			},
		},
	}
	AnthropicEventToResponsesEvents(&start, state)
	assert.Equal(t, 40, state.CacheCreationInputTokens)

	delta := AnthropicStreamEvent{
		Type:  "message_delta",
		Usage: &AnthropicUsage{CacheCreation: &AnthropicCacheCreation{Ephemeral1hInputTokens: &oneHour}},
	}
	AnthropicEventToResponsesEvents(&delta, state)
	assert.Equal(t, 50, state.CacheCreationInputTokens)
}

// A total resolved outside the stream (the gateway's normalized usage) counts as
// declared, so later stale details cannot recompute it away.
func TestAnthropicStateSetCacheCreationTotalIsAuthoritative(t *testing.T) {
	zero := 0
	state := NewAnthropicEventToResponsesState()
	state.SetCacheCreationTotal(321)
	state.mergeCacheCreationUsage(AnthropicUsage{
		CacheCreation: &AnthropicCacheCreation{Ephemeral5mInputTokens: &zero},
	})
	assert.Equal(t, 321, state.CacheCreationInputTokens)
}

// A positive flat field outranks the breakdown, and an explicit zero flat field
// with no breakdown stays zero rather than falling back to a stale detail.
func TestAnthropicCacheCreationInputTokensPrecedence(t *testing.T) {
	fiveMinutes := 5
	oneHour := 7

	assert.Equal(t, 100, anthropicCacheCreationInputTokens(AnthropicUsage{
		CacheCreationInputTokens: 100,
		CacheCreation:            &AnthropicCacheCreation{Ephemeral5mInputTokens: &fiveMinutes},
	}))
	assert.Equal(t, 12, anthropicCacheCreationInputTokens(AnthropicUsage{
		CacheCreation: &AnthropicCacheCreation{Ephemeral5mInputTokens: &fiveMinutes, Ephemeral1hInputTokens: &oneHour},
	}))
	assert.Equal(t, 0, anthropicCacheCreationInputTokens(AnthropicUsage{}))
}

// The decoder keeps a zero class distinct from an omitted one.
func TestAnthropicUsageDecodesExplicitZeroCacheClass(t *testing.T) {
	var usage AnthropicUsage
	require.NoError(t, json.Unmarshal([]byte(`{"cache_creation":{"ephemeral_1h_input_tokens":0}}`), &usage))
	require.NotNil(t, usage.CacheCreation)
	require.NotNil(t, usage.CacheCreation.Ephemeral1hInputTokens)
	assert.Equal(t, 0, *usage.CacheCreation.Ephemeral1hInputTokens)
	assert.Nil(t, usage.CacheCreation.Ephemeral5mInputTokens)
}
