//go:build unit || !integration

package apicompat

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAnthropicUsageFromResponsesUsage_CacheCreation(t *testing.T) {
	usage := &ResponsesUsage{
		InputTokens:              20,
		OutputTokens:             5,
		CacheCreationInputTokens: 6,
		InputTokensDetails: &ResponsesInputTokensDetails{
			CachedTokens: 4,
		},
	}

	got := anthropicUsageFromResponsesUsage(usage)

	assert.Equal(t, 10, got.InputTokens, "input = total(20) - cache_read(4) - cache_creation(6)")
	assert.Equal(t, 5, got.OutputTokens)
	assert.Equal(t, 4, got.CacheReadInputTokens)
	assert.Equal(t, 6, got.CacheCreationInputTokens, "cache creation must be preserved")
}

func TestAnthropicUsageFromResponsesUsage_NoCacheCreation(t *testing.T) {
	usage := &ResponsesUsage{
		InputTokens:  10,
		OutputTokens: 5,
		InputTokensDetails: &ResponsesInputTokensDetails{
			CachedTokens: 3,
		},
	}

	got := anthropicUsageFromResponsesUsage(usage)

	assert.Equal(t, 7, got.InputTokens)
	assert.Equal(t, 3, got.CacheReadInputTokens)
	assert.Equal(t, 0, got.CacheCreationInputTokens)
}

func TestResponsesEventToAnthropicEvents_StreamingCacheCreation(t *testing.T) {
	state := NewResponsesEventToAnthropicState()
	state.MessageStartSent = true

	completedEvt := &ResponsesStreamEvent{
		Type: "response.completed",
		Response: &ResponsesResponse{
			Status: "completed",
			Usage: &ResponsesUsage{
				InputTokens:              20,
				OutputTokens:             5,
				CacheCreationInputTokens: 6,
				InputTokensDetails: &ResponsesInputTokensDetails{
					CachedTokens: 4,
				},
			},
		},
	}

	events := ResponsesEventToAnthropicEvents(completedEvt, state)

	var deltaEvt *AnthropicStreamEvent
	for i := range events {
		if events[i].Type == "message_delta" {
			deltaEvt = &events[i]
			break
		}
	}
	require.NotNil(t, deltaEvt, "should have message_delta event")
	require.NotNil(t, deltaEvt.Usage)
	assert.Equal(t, 6, deltaEvt.Usage.CacheCreationInputTokens, "streaming cache_creation must be preserved")
	assert.Equal(t, 10, deltaEvt.Usage.InputTokens, "input = 20 - 4(read) - 6(creation)")
	assert.Equal(t, 4, deltaEvt.Usage.CacheReadInputTokens)
}

func TestAnthropicToResponsesResponse_CacheCreation(t *testing.T) {
	resp := AnthropicResponse{
		ID:    "msg_test",
		Type:  "message",
		Role:  "assistant",
		Model: "claude-opus-4-6",
		Usage: AnthropicUsage{
			InputTokens:              10,
			OutputTokens:             5,
			CacheReadInputTokens:     4,
			CacheCreationInputTokens: 6,
		},
		StopReason: AnthropicStopReasonPtr("end_turn"),
	}

	out := AnthropicToResponsesResponse(&resp)

	require.NotNil(t, out.Usage)
	assert.Equal(t, 20, out.Usage.InputTokens, "total = input(10) + cache_read(4) + cache_creation(6)")
	assert.Equal(t, 6, out.Usage.CacheCreationInputTokens, "cache creation must round-trip")
}

func TestAnthropicUsageDecodesCacheCreationBreakdown(t *testing.T) {
	var usage AnthropicUsage
	require.NoError(t, json.Unmarshal([]byte(`{"input_tokens":10,"cache_creation":{"ephemeral_5m_input_tokens":0,"ephemeral_1h_input_tokens":7}}`), &usage))
	require.NotNil(t, usage.CacheCreation)
	require.NotNil(t, usage.CacheCreation.Ephemeral5mInputTokens)
	require.NotNil(t, usage.CacheCreation.Ephemeral1hInputTokens)
	assert.Equal(t, 0, *usage.CacheCreation.Ephemeral5mInputTokens)
	assert.Equal(t, 7, *usage.CacheCreation.Ephemeral1hInputTokens)
}

// The breakdown is the provider's own split and must win over the flat field,
// which can be zero while the classes carry the write.
func TestAnthropicToResponsesResponse_UsesCacheCreationBreakdown(t *testing.T) {
	fiveMinutes := 2
	oneHour := 3
	resp := AnthropicResponse{Usage: AnthropicUsage{
		InputTokens: 10,
		CacheCreation: &AnthropicCacheCreation{
			Ephemeral5mInputTokens: &fiveMinutes,
			Ephemeral1hInputTokens: &oneHour,
		},
	}}

	out := AnthropicToResponsesResponse(&resp)
	require.NotNil(t, out.Usage)
	assert.Equal(t, 15, out.Usage.InputTokens)
	assert.Equal(t, 5, out.Usage.CacheCreationInputTokens)
}

// An explicit zero class stays zero: `{"ephemeral_1h_input_tokens":0}` means no
// 1h write, not "field omitted".
func TestAnthropicToResponsesResponse_ZeroBreakdownClass(t *testing.T) {
	zero := 0
	fiveMinutes := 4
	resp := AnthropicResponse{Usage: AnthropicUsage{
		InputTokens: 10,
		CacheCreation: &AnthropicCacheCreation{
			Ephemeral5mInputTokens: &fiveMinutes,
			Ephemeral1hInputTokens: &zero,
		},
	}}

	out := AnthropicToResponsesResponse(&resp)
	require.NotNil(t, out.Usage)
	assert.Equal(t, 4, out.Usage.CacheCreationInputTokens)
	assert.Equal(t, 14, out.Usage.InputTokens)
}

func TestAnthropicEventToResponsesEvents_MergesCacheCreationBreakdown(t *testing.T) {
	tests := []struct {
		name      string
		delta     string
		wantTotal int
	}{
		{"explicit_1h_zero", `{"cache_creation":{"ephemeral_1h_input_tokens":0}}`, 2},
		{"explicit_5m_zero", `{"cache_creation":{"ephemeral_5m_input_tokens":0}}`, 3},
		{"omitted_details", `{}`, 5},
		{"empty_details", `{"cache_creation":{}}`, 5},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			state := NewAnthropicEventToResponsesState()
			two := 2
			three := 3
			start := AnthropicStreamEvent{
				Type: "message_start",
				Message: &AnthropicResponse{
					ID:    "msg_cache",
					Model: "claude-sonnet-4",
					Usage: AnthropicUsage{
						InputTokens: 10,
						CacheCreation: &AnthropicCacheCreation{
							Ephemeral5mInputTokens: &two,
							Ephemeral1hInputTokens: &three,
						},
					},
				},
			}
			AnthropicEventToResponsesEvents(&start, state)

			var delta AnthropicStreamEvent
			require.NoError(t, json.Unmarshal([]byte(`{"type":"message_delta","usage":`+tt.delta+`}`), &delta))
			AnthropicEventToResponsesEvents(&delta, state)

			assert.Equal(t, tt.wantTotal, state.CacheCreationInputTokens)
		})
	}
}
