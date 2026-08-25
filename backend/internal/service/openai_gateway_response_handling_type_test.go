package service

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestOpenAIStreamEventIsTerminalWithTypeMatchesExistingSemantics(t *testing.T) {
	tests := []struct {
		name string
		data string
		want bool
	}{
		{name: "empty", data: "", want: false},
		{name: "whitespace", data: " \t ", want: false},
		{name: "done", data: " [DONE] ", want: true},
		{name: "JSON outer whitespace", data: " \n\t {\"type\":\"response.completed\"} \r\n", want: true},
		{name: "completed", data: `{"type":"response.completed"}`, want: true},
		{name: "response done", data: `{"type":"response.done"}`, want: true},
		{name: "failed", data: `{"type":"response.failed"}`, want: true},
		{name: "incomplete", data: `{"type":"response.incomplete"}`, want: true},
		{name: "cancelled", data: `{"type":"response.cancelled"}`, want: true},
		{name: "canceled", data: `{"type":"response.canceled"}`, want: true},
		{name: "delta", data: `{"type":"response.output_text.delta"}`, want: false},
		{name: "invalid JSON", data: `{"type":`, want: false},
		{name: "terminal with trailing garbage", data: `{"type":"response.completed"} trailing`, want: true},
		{name: "nonterminal with trailing garbage", data: `{"type":"response.output_text.delta"} trailing`, want: false},
		{name: "type whitespace is normalized", data: `{"type":" response.completed "}`, want: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			eventType := gjson.GetBytes([]byte(tt.data), "type").String()
			got := openAIStreamEventIsTerminalWithType(tt.data, eventType)

			require.Equal(t, tt.want, got)
			require.Equal(t, openAIStreamEventIsTerminal(tt.data), got)
		})
	}
}

func TestResponsesStreamEventMayContributeToOutputMatchesAccumulatorEvents(t *testing.T) {
	contributing := []string{
		"response.output_text.delta",
		"response.output_text.done",
		"response.refusal.delta",
		"response.refusal.done",
		"response.content_part.added",
		"response.content_part.done",
		"response.reasoning_summary_part.added",
		"response.reasoning_summary_part.done",
		"response.output_item.added",
		"response.output_item.done",
		"response.function_call_arguments.delta",
		"response.function_call_arguments.done",
		"response.custom_tool_call_input.delta",
		"response.custom_tool_call_input.done",
		"response.tool_search_call_arguments.delta",
		"response.tool_search_call_arguments.done",
		"response.reasoning_summary_text.delta",
		"response.reasoning_summary_text.done",
		"response.reasoning_text.delta",
		"response.reasoning_text.done",
	}
	for _, eventType := range contributing {
		require.Truef(t, responsesStreamEventMayContributeToOutput(eventType), "%s must reach the output accumulator", eventType)
	}

	for _, eventType := range []string{"", "response.created", "response.completed", "response.failed", "response.image_generation_call.partial_image"} {
		require.Falsef(t, responsesStreamEventMayContributeToOutput(eventType), "%s is handled outside the text/tool accumulator", eventType)
	}
}

var (
	benchmarkOpenAIResponseSSEEventTypeSink string
	benchmarkOpenAIResponseSSETerminalSink  bool
)

func BenchmarkOpenAIResponseSSETypeExtraction(b *testing.B) {
	data := `{"type":"response.output_text.delta","sequence_number":42,"delta":"streaming response benchmark payload"}`
	dataBytes := []byte(data)

	b.Run("legacy double parse", func(b *testing.B) {
		b.ReportAllocs()
		b.SetBytes(int64(len(dataBytes)))
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			benchmarkOpenAIResponseSSETerminalSink = openAIStreamEventIsTerminal(data)
			benchmarkOpenAIResponseSSEEventTypeSink = strings.TrimSpace(gjson.GetBytes(dataBytes, "type").String())
		}
	})

	b.Run("reused single parse", func(b *testing.B) {
		b.ReportAllocs()
		b.SetBytes(int64(len(dataBytes)))
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			eventTypeRaw := gjson.GetBytes(dataBytes, "type").String()
			benchmarkOpenAIResponseSSEEventTypeSink = strings.TrimSpace(eventTypeRaw)
			benchmarkOpenAIResponseSSETerminalSink = openAIStreamEventIsTerminalWithType(data, eventTypeRaw)
		}
	})
}
