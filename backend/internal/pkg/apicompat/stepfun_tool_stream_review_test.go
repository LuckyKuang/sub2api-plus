//go:build unit

package apicompat

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Contract: https://platform.claude.com/docs/en/build-with-claude/streaming
// ("Event types" and "Input JSON delta"). A content block's deltas belong
// between its start and stop; the complete tool input must be an object when
// that block stops. Step's documented Chat bridge also accepts reasoning,
// visible content/refusal and independently indexed tool argument fragments.
// A later Chat delta cannot mutate an already finalized Messages block.
func TestStepFunReviewToolStreamKeepsClosedBlocksImmutable(t *testing.T) {
	firstTool := `{"id":"chat_step_review","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_weather","type":"function","function":{"name":"weather","arguments":"{\"city\":"}}]}}]}`
	lastTool := `{"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"\"Paris\"}"}}]}}]}`
	otherTool := `{"choices":[{"index":0,"delta":{"tool_calls":[{"index":1,"id":"call_clock","type":"function","function":{"name":"clock","arguments":"{\"zone\":\"UTC\"}"}}]}}]}`
	finish := `{"choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}],"usage":{"prompt_tokens":7,"completion_tokens":5,"total_tokens":12}}`
	for _, tc := range []struct {
		name, middle, wantText, wantReasoning string
		beforeTool, parallel                  bool
	}{
		{name: "ordinary_text_before_tool", middle: `{"choices":[{"index":0,"delta":{"content":"I will check."}}]}`, wantText: "I will check.", beforeTool: true},
		{name: "ordinary_parallel_tools", parallel: true},
		{name: "text_between_argument_fragments", middle: `{"choices":[{"index":0,"delta":{"content":"I will check."}}]}`, wantText: "I will check."},
		{name: "refusal_between_argument_fragments", middle: `{"choices":[{"index":0,"delta":{"refusal":"Some details are unavailable."}}]}`, wantText: "Some details are unavailable."},
		{name: "reasoning_between_argument_fragments", middle: `{"choices":[{"index":0,"delta":{"reasoning_content":"Check the selected city."}}]}`, wantReasoning: "Check the selected city."},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var chunks []string
			if tc.beforeTool {
				chunks = append(chunks, tc.middle)
			}
			chunks = append(chunks, firstTool)
			if !tc.beforeTool && tc.middle != "" {
				chunks = append(chunks, tc.middle)
			}
			if tc.parallel {
				chunks = append(chunks, otherTool)
			}
			chunks = append(chunks, lastTool, finish)
			events := collectAnthropicStreamEvents(t, chunks)

			type clientBlock struct {
				kind, id, name, arguments string
				open                      bool
				starts, stops             int
			}
			blocks := map[int]*clientBlock{}
			var text, reasoning string
			messageStarts, messageStops := 0, 0
			for position, event := range events {
				switch event.Type {
				case "message_start":
					messageStarts++
				case "content_block_start":
					require.NotNil(t, event.Index)
					require.NotNil(t, event.ContentBlock)
					index := *event.Index
					_, exists := blocks[index]
					require.False(t, exists, "block %d cannot be reopened", index)
					blocks[index] = &clientBlock{kind: event.ContentBlock.Type, id: event.ContentBlock.ID, name: event.ContentBlock.Name, open: true, starts: 1}
				case "content_block_delta":
					require.NotNil(t, event.Index)
					require.NotNil(t, event.Delta)
					block := blocks[*event.Index]
					require.NotNil(t, block)
					assert.True(t, block.open, "event %d mutates stopped block %d", position, *event.Index)
					switch event.Delta.Type {
					case "input_json_delta":
						assert.Equal(t, "tool_use", block.kind)
						block.arguments += event.Delta.PartialJSON
					case "text_delta":
						assert.Equal(t, "text", block.kind)
						text += event.Delta.Text
					case "thinking_delta":
						assert.Equal(t, "thinking", block.kind)
						reasoning += event.Delta.Thinking
					}
				case "content_block_stop":
					require.NotNil(t, event.Index)
					block := blocks[*event.Index]
					require.NotNil(t, block)
					assert.True(t, block.open, "block %d stops exactly once", *event.Index)
					if block.kind == "tool_use" {
						assert.True(t, json.Valid([]byte(block.arguments)), "tool %s input must be complete when its block stops: %s", block.id, block.arguments)
					}
					block.open = false
					block.stops++
				case "message_stop":
					messageStops++
					for index, block := range blocks {
						assert.False(t, block.open, "block %d must stop before message_stop", index)
					}
				}
			}
			require.Equal(t, 1, messageStarts)
			require.Equal(t, 1, messageStops)
			require.Equal(t, tc.wantText, text)
			require.Equal(t, tc.wantReasoning, reasoning)
			toolInputs := map[string]string{}
			toolNames := map[string]string{}
			for _, block := range blocks {
				require.Equal(t, 1, block.starts)
				require.Equal(t, 1, block.stops)
				if block.kind == "tool_use" {
					_, duplicate := toolInputs[block.id]
					require.False(t, duplicate, "a Chat call must produce one Messages tool_use")
					toolInputs[block.id], toolNames[block.id] = block.arguments, block.name
				}
			}
			require.Equal(t, "weather", toolNames["call_weather"])
			require.JSONEq(t, `{"city":"Paris"}`, toolInputs["call_weather"])
			if tc.parallel {
				require.Len(t, toolInputs, 2)
				require.Equal(t, "clock", toolNames["call_clock"])
				require.JSONEq(t, `{"zone":"UTC"}`, toolInputs["call_clock"])
			} else {
				require.Len(t, toolInputs, 1)
			}
		})
	}
}
