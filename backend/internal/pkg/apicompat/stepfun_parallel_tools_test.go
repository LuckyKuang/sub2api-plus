//go:build unit

package apicompat

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestStepFunParallelChatToolsKeepIndependentAnthropicBlockLifecycles(t *testing.T) {
	events := collectAnthropicStreamEvents(t, []string{
		`{"id":"chat_step_tools","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_weather","type":"function","function":{"name":"weather","arguments":""}},{"index":1,"id":"call_clock","type":"function","function":{"name":"clock","arguments":""}}]}}]}`,
		`{"choices":[{"index":0,"delta":{"tool_calls":[{"index":1,"function":{"arguments":"{\"zone\":"}}]}}]}`,
		`{"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"{\"city\":"}}]}}]}`,
		`{"choices":[{"index":0,"delta":{"tool_calls":[{"index":1,"function":{"arguments":"\"UTC\"}"}}]}}]}`,
		`{"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"\"Paris\"}"}}]}}]}`,
		`{"choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}],"usage":{"prompt_tokens":7,"completion_tokens":5,"total_tokens":12}}`,
	})
	open := map[int]bool{}
	starts, stops := map[int]int{}, map[int]int{}
	for _, event := range events {
		switch event.Type {
		case "content_block_start":
			require.NotNil(t, event.Index)
			starts[*event.Index]++
			require.False(t, open[*event.Index])
			open[*event.Index] = true
		case "content_block_delta":
			require.NotNil(t, event.Index)
			require.True(t, open[*event.Index], "a tool argument delta cannot arrive after content_block_stop")
		case "content_block_stop":
			require.NotNil(t, event.Index)
			require.True(t, open[*event.Index])
			stops[*event.Index]++
			open[*event.Index] = false
		case "message_stop":
			for _, active := range open {
				require.False(t, active, "message_stop requires every content block to close")
			}
		}
	}
	require.Len(t, starts, 2)
	require.Equal(t, map[int]int{0: 1, 1: 1}, starts)
	require.Equal(t, starts, stops)
	tools := assembleToolUseBlocks(events)
	require.Len(t, tools, 2)
	require.Equal(t, "call_weather", tools[0].ID)
	require.Equal(t, "weather", tools[0].Name)
	require.JSONEq(t, `{"city":"Paris"}`, tools[0].Input)
	require.Equal(t, "call_clock", tools[1].ID)
	require.Equal(t, "clock", tools[1].Name)
	require.JSONEq(t, `{"zone":"UTC"}`, tools[1].Input)
}
