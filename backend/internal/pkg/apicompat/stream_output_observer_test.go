package apicompat

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestObserveResponsesOutput(t *testing.T) {
	tests := []struct {
		name    string
		payload string
		want    StreamOutputObservation
	}{
		{name: "created lifecycle", payload: `{"type":"response.created","response":{"id":"r1"}}`},
		{name: "empty text delta", payload: `{"type":"response.output_text.delta","delta":""}`},
		{name: "whitespace text delta", payload: `{"type":"response.output_text.delta","delta":" "}`, want: streamOutput(StreamOutputText, true)},
		{name: "text done aggregate", payload: `{"type":"response.output_text.done","text":"done"}`, want: streamOutput(StreamOutputText, false)},
		{name: "refusal delta", payload: `{"type":"response.refusal.delta","delta":"no"}`, want: streamOutput(StreamOutputText, true)},
		{name: "reasoning delta", payload: `{"type":"response.reasoning_text.delta","delta":"plan"}`, want: streamOutput(StreamOutputReasoning, true)},
		{name: "reasoning done aggregate", payload: `{"type":"response.reasoning_summary_text.done","text":"plan"}`, want: streamOutput(StreamOutputReasoning, false)},
		{name: "reasoning summary part", payload: `{"type":"response.reasoning_summary_part.done","part":{"type":"summary_text","text":"plan"}}`, want: streamOutput(StreamOutputReasoning, false)},
		{name: "text annotation", payload: `{"type":"response.output_text.annotation.added","annotation":{"type":"url_citation","url":"https://example.test"}}`, want: streamOutput(StreamOutputText, false)},
		{name: "empty text annotation", payload: `{"type":"response.output_text.annotation.added","annotation":{}}`},
		{name: "nested empty text annotation", payload: `{"type":"response.output_text.annotation.added","annotation":{"sources":[]}}`},
		{name: "tool arguments delta", payload: `{"type":"response.function_call_arguments.delta","delta":"{\"q\":"}`, want: streamOutput(StreamOutputTool, true)},
		{name: "tool arguments done aggregate", payload: `{"type":"response.function_call_arguments.done","arguments":"{\"q\":1}"}`, want: streamOutput(StreamOutputTool, false)},
		{name: "tool search object delta", payload: `{"type":"response.tool_search_call_arguments.delta","arguments":{"query":"docs"}}`, want: streamOutput(StreamOutputTool, true)},
		{name: "tool search object done", payload: `{"type":"response.tool_search_call_arguments.done","arguments":{"query":"docs"}}`, want: streamOutput(StreamOutputTool, false)},
		{name: "MCP arguments delta", payload: `{"type":"response.mcp_call_arguments.delta","delta":"{\"path\":"}`, want: streamOutput(StreamOutputTool, true)},
		{name: "empty MCP arguments delta", payload: `{"type":"response.mcp_call_arguments.delta","delta":""}`},
		{name: "MCP arguments done", payload: `{"type":"response.mcp_call_arguments.done","arguments":"{\"path\":\"README.md\"}"}`, want: streamOutput(StreamOutputTool, false)},
		{name: "code interpreter code delta", payload: `{"type":"response.code_interpreter_call_code.delta","delta":"print('hello')"}`, want: streamOutput(StreamOutputTool, true)},
		{name: "empty code interpreter code delta", payload: `{"type":"response.code_interpreter_call_code.delta","delta":""}`},
		{name: "code interpreter code done", payload: `{"type":"response.code_interpreter_call_code.done","code":"print('done')"}`, want: streamOutput(StreamOutputTool, false)},
		{name: "tool search item", payload: `{"type":"response.output_item.done","item":{"type":"tool_search_call","name":"tool_search","arguments":{"query":"docs"}}}`, want: streamOutput(StreamOutputTool, false)},
		{name: "local shell item", payload: `{"type":"response.output_item.added","item":{"type":"local_shell_call","call_id":"call_1","action":{"command":"pwd"}}}`, want: streamOutput(StreamOutputTool, true)},
		{name: "shell item", payload: `{"type":"response.output_item.added","item":{"type":"shell_call","call_id":"call_1","action":{"commands":["pwd"]}}}`, want: streamOutput(StreamOutputTool, true)},
		{name: "apply patch item", payload: `{"type":"response.output_item.done","item":{"type":"apply_patch_call","operation":{"type":"update_file","path":"README.md","diff":"@@"}}}`, want: streamOutput(StreamOutputTool, false)},
		{name: "MCP tools list", payload: `{"type":"response.output_item.done","item":{"type":"mcp_list_tools","server_label":"docs","tools":[{"name":"search"}]}}`, want: streamOutput(StreamOutputTool, false)},
		{name: "empty MCP tools list", payload: `{"type":"response.output_item.done","item":{"type":"mcp_list_tools","server_label":"docs","tools":[]}}`},
		{name: "web search status only", payload: `{"type":"response.output_item.added","item":{"type":"web_search_call","status":"searching"}}`},
		{name: "web search action", payload: `{"type":"response.output_item.done","item":{"type":"web_search_call","action":{"type":"search","query":"docs"}}}`, want: streamOutput(StreamOutputTool, false)},
		{name: "empty image partial", payload: `{"type":"response.image_generation_call.partial_image","partial_image_b64":""}`},
		{name: "image partial", payload: `{"type":"response.image_generation_call.partial_image","partial_image_b64":"aGVsbG8="}`, want: streamOutput(StreamOutputImage, false)},
		{name: "terminal without output", payload: `{"type":"response.completed","response":{"status":"completed","output":[]}}`},
		{name: "terminal text aggregate", payload: `{"type":"response.completed","response":{"output":[{"type":"message","content":[{"type":"output_text","text":"done"}]}]}}`, want: streamOutput(StreamOutputText, false)},
		{name: "terminal refusal aggregate", payload: `{"type":"response.completed","response":{"output":[{"type":"message","content":[{"type":"refusal","refusal":"no"}]}]}}`, want: streamOutput(StreamOutputText, false)},
		{name: "terminal annotation-only text", payload: `{"type":"response.completed","response":{"output":[{"type":"message","content":[{"type":"output_text","text":"","annotations":[{"type":"url_citation","url":"https://example.test"}]}]}]}}`, want: streamOutput(StreamOutputText, false)},
		{name: "terminal empty reasoning summary", payload: `{"type":"response.completed","response":{"output":[{"type":"reasoning","summary":[{"type":"summary_text","text":""}]}]}}`},
		{name: "terminal reasoning summary", payload: `{"type":"response.completed","response":{"output":[{"type":"reasoning","summary":[{"type":"summary_text","text":"plan"}]}]}}`, want: streamOutput(StreamOutputReasoning, false)},
		{name: "terminal encrypted reasoning", payload: `{"type":"response.completed","response":{"output":[{"type":"reasoning","summary":[],"encrypted_content":"enc"}]}}`, want: streamOutput(StreamOutputReasoning, false)},
		{name: "output item final image", payload: `{"type":"response.output_item.done","item":{"type":"image_generation_call","result":"aW1hZ2U="}}`, want: streamOutput(StreamOutputImage, false)},
		{name: "output item blank final image", payload: `{"type":"response.output_item.done","item":{"type":"image_generation_call","result":" "}}`},
		{name: "legacy output audio delta", payload: `{"type":"response.output_audio.delta","delta":"YXVkaW8="}`, want: streamOutput(StreamOutputAudio, false)},
		{name: "audio delta", payload: `{"type":"response.audio.delta","delta":"YXVkaW8="}`, want: streamOutput(StreamOutputAudio, false)},
		{name: "audio done without bytes", payload: `{"type":"response.audio.done","response_id":"resp_123"}`},
		{name: "audio transcript delta", payload: `{"type":"response.audio.transcript.delta","delta":"hello"}`, want: streamOutput(StreamOutputText, true)},
		{name: "audio transcript done", payload: `{"type":"response.audio.transcript.done","transcript":"hello"}`, want: streamOutput(StreamOutputText, false)},
		{name: "terminal output audio", payload: `{"type":"response.completed","response":{"output":[{"type":"output_audio","data":"YXVkaW8=","transcript":"hello"}]}}`, want: streamOutput(StreamOutputAudio, false)},
		{name: "terminal transcript-only output audio", payload: `{"type":"response.completed","response":{"output":[{"type":"output_audio","data":"","transcript":"hello"}]}}`, want: streamOutput(StreamOutputText, false)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, ObserveResponsesOutput([]byte(tt.payload)))
		})
	}
}

func TestObserveChatChunkOutput(t *testing.T) {
	finish := "stop"
	empty := ""
	space := " "
	tests := []struct {
		name  string
		chunk ChatCompletionsChunk
		want  StreamOutputObservation
	}{
		{name: "usage only", chunk: ChatCompletionsChunk{Usage: &ChatUsage{CompletionTokens: 1}}},
		{name: "role only", chunk: ChatCompletionsChunk{Choices: []ChatChunkChoice{{Delta: ChatDelta{Role: "assistant"}}}}},
		{name: "finish only", chunk: ChatCompletionsChunk{Choices: []ChatChunkChoice{{Delta: ChatDelta{Content: &empty}, FinishReason: &finish}}}},
		{name: "whitespace token", chunk: ChatCompletionsChunk{Choices: []ChatChunkChoice{{Delta: ChatDelta{Content: &space}}}}, want: streamOutput(StreamOutputText, true)},
		{name: "refusal token", chunk: ChatCompletionsChunk{Choices: []ChatChunkChoice{{Delta: ChatDelta{Refusal: &space}}}}, want: streamOutput(StreamOutputText, true)},
		{name: "terminal aggregate text", chunk: ChatCompletionsChunk{AggregateOutput: true, Choices: []ChatChunkChoice{{Delta: ChatDelta{Content: &space}}}}, want: streamOutput(StreamOutputText, false)},
		{name: "legacy function call", chunk: ChatCompletionsChunk{Choices: []ChatChunkChoice{{Delta: ChatDelta{FunctionCall: &ChatFunctionCall{Name: "search"}}}}}, want: streamOutput(StreamOutputTool, true)},
		{
			name: "tool index only",
			chunk: ChatCompletionsChunk{Choices: []ChatChunkChoice{{
				Delta: ChatDelta{ToolCalls: []ChatToolCall{{Index: intPtr(0), ID: "call_1"}}},
			}}},
		},
		{
			name: "tool name",
			chunk: ChatCompletionsChunk{Choices: []ChatChunkChoice{{
				Delta: ChatDelta{ToolCalls: []ChatToolCall{{Function: ChatFunctionCall{Name: "search"}}}},
			}}},
			want: streamOutput(StreamOutputTool, true),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, ObserveChatChunkOutput(&tt.chunk))
		})
	}
}

func TestObserveAnthropicOutput(t *testing.T) {
	require.Equal(t, StreamOutputObservation{}, ObserveAnthropicOutput(&AnthropicStreamEvent{Type: "message_start"}))
	require.Equal(t, StreamOutputObservation{}, ObserveAnthropicOutput(&AnthropicStreamEvent{Type: "message_stop"}))
	require.Equal(t, streamOutput(StreamOutputReasoning, true), ObserveAnthropicOutput(&AnthropicStreamEvent{
		Type:  "content_block_delta",
		Delta: &AnthropicDelta{Type: "thinking_delta", Thinking: "plan"},
	}))
	require.Equal(t, streamOutput(StreamOutputTool, true), ObserveAnthropicOutput(&AnthropicStreamEvent{
		Type:         "content_block_start",
		ContentBlock: &AnthropicContentBlock{Type: "tool_use", Name: "lookup", Input: json.RawMessage(`{}`)},
	}))
	require.Equal(t, streamOutput(StreamOutputTool, true), ObserveAnthropicOutput(&AnthropicStreamEvent{
		Type:         "content_block_start",
		ContentBlock: &AnthropicContentBlock{Type: "server_tool_use", Name: "web_search", Input: json.RawMessage(`{"q":"docs"}`)},
	}))
	require.Equal(t, streamOutput(StreamOutputTool, false), ObserveAnthropicOutput(&AnthropicStreamEvent{
		Type:         "content_block_start",
		ContentBlock: &AnthropicContentBlock{Type: "web_search_tool_result", Content: json.RawMessage(`[{"title":"Docs"}]`)},
	}))
	require.Equal(t, streamOutput(StreamOutputReasoning, false), ObserveAnthropicOutput(&AnthropicStreamEvent{
		Type:  "content_block_delta",
		Delta: &AnthropicDelta{Type: "signature_delta", Signature: "enc"},
	}))
	require.Equal(t, streamOutput(StreamOutputText, false), ObserveAnthropicOutput(&AnthropicStreamEvent{
		Type:  "content_block_delta",
		Delta: &AnthropicDelta{Type: "citations_delta", Citation: json.RawMessage(`{"type":"char_location","cited_text":"docs"}`)},
	}))
	require.Equal(t, StreamOutputObservation{}, ObserveAnthropicOutput(&AnthropicStreamEvent{
		Type:  "content_block_delta",
		Delta: &AnthropicDelta{Type: "citations_delta", Citation: json.RawMessage(`{"sources":[]}`)},
	}))
	require.Equal(t, streamOutput(StreamOutputReasoning, false), ObserveAnthropicOutput(&AnthropicStreamEvent{
		Type:         "content_block_start",
		ContentBlock: &AnthropicContentBlock{Type: "redacted_thinking", Data: "enc"},
	}))
}

func TestObserveGeminiOutput(t *testing.T) {
	require.Equal(t, StreamOutputObservation{}, ObserveGeminiOutput([]byte(`{"candidates":[{"finishReason":"STOP"}],"usageMetadata":{"candidatesTokenCount":1}}`)))
	require.Equal(t, streamOutput(StreamOutputText, true), ObserveGeminiOutput([]byte(`{"candidates":[{"content":{"parts":[{"text":" "}]}}]}`)))
	require.Equal(t, streamOutput(StreamOutputTool, true), ObserveGeminiOutput([]byte(`{"candidates":[{"content":{"parts":[{"functionCall":{"name":"search","args":{}}}]}}]}`)))
	require.Equal(t, streamOutput(StreamOutputTool, true), ObserveGeminiOutput([]byte(`{"candidates":[{"content":{"parts":[{"function_call":{"name":"search","args":{}}}]}}]}`)))
	require.Equal(t, streamOutput(StreamOutputTool, true), ObserveGeminiOutput([]byte(`{"candidates":[{"content":{"parts":[{"executableCode":{"language":"PYTHON","code":"print(1)"}}]}}]}`)))
	require.Equal(t, streamOutput(StreamOutputTool, false), ObserveGeminiOutput([]byte(`{"candidates":[{"content":{"parts":[{"codeExecutionResult":{"outcome":"OUTCOME_OK","output":"1"}}]}}]}`)))
	require.Equal(t, streamOutput(StreamOutputImage, false), ObserveGeminiOutput([]byte(`{"candidates":[{"content":{"parts":[{"inlineData":{"mimeType":"image/png","data":"aW1hZ2U="}}]}}]}`)))
	require.Equal(t, streamOutput(StreamOutputAudio, false), ObserveGeminiOutput([]byte(`{"response":{"candidates":[{"content":{"parts":[{"inlineData":{"mimeType":"audio/wav","data":"YXVkaW8="}}]}}]}}`)))
	require.Equal(t, streamOutput(StreamOutputImage, false), ObserveGeminiOutput([]byte(`{"candidates":[{"content":{"parts":[{"inline_data":{"mime_type":"image/webp","data":"aW1hZ2U="}}]}}]}`)))
	require.Equal(t, streamOutput(StreamOutputAudio, false), ObserveGeminiOutput([]byte(`{"candidates":[{"content":{"parts":[{"fileData":{"mimeType":"audio/wav","fileUri":"gs://bucket/audio.wav"}}]}}]}`)))
	require.Equal(t, streamOutput(StreamOutputImage, false), ObserveGeminiOutput([]byte(`{"candidates":[{"content":{"parts":[{"file_data":{"mime_type":" IMAGE/PNG ","file_uri":"gs://bucket/image.png"}}]}}]}`)))
	require.Equal(t, streamOutput(StreamOutputText, false), ObserveGeminiOutput([]byte(`{"candidates":[{"content":{"parts":[]},"groundingMetadata":{"webSearchQueries":["docs"]}}]}`)))
	require.Equal(t, streamOutput(StreamOutputText, false), ObserveGeminiOutput([]byte(`{"response":{"candidates":[{"citation_metadata":{"citationSources":[{"uri":"https://example.test"}]}}]}}`)))
	require.Equal(t, StreamOutputObservation{}, ObserveGeminiOutput([]byte(`{"candidates":[{"groundingMetadata":{"webSearchQueries":[],"groundingChunks":[]}}]}`)))
	require.Equal(t, StreamOutputObservation{}, ObserveGeminiOutput([]byte(`{"candidates":[{"content":{"parts":[{"fileData":{"mimeType":"video/mp4","fileUri":"gs://bucket/video.mp4"}}]}}]}`)))
	require.Equal(t, StreamOutputObservation{}, ObserveGeminiOutput([]byte(`{"candidates":[{"content":{"parts":[{"fileData":{"fileUri":"gs://bucket/unknown"}}]}}]}`)))
	require.Equal(t, streamOutput(StreamOutputText, true), ObserveGeminiOutput([]byte(`{"candidates":[{"content":{"parts":[{"fileData":{"mimeType":"video/mp4","fileUri":"gs://bucket/video.mp4"}},{"text":"after video"}]}}]}`)))
	require.Equal(t, streamOutput(StreamOutputAudio, false), ObserveGeminiOutput([]byte(`{"candidates":[{"content":{"parts":[{"inlineData":{"mimeType":"application/octet-stream","data":"dW5rbm93bg=="}}]}},{"content":{"parts":[{"inlineData":{"mimeType":"audio/wav","data":"YXVkaW8="}}]}}]}`)))
	require.Equal(t, streamOutput(StreamOutputText, false), ObserveGeminiOutput([]byte(`{"candidates":[{"content":{"parts":[{"fileData":{"fileUri":"gs://bucket/unknown"}}]},"groundingMetadata":{"webSearchQueries":["docs"]}}]}`)))
	require.Equal(t, streamOutput(StreamOutputReasoning, false), ObserveGeminiOutput([]byte(`{"candidates":[{"content":{"parts":[{"thought":true,"thoughtSignature":"enc"}]}}]}`)))
	require.Equal(t, StreamOutputObservation{}, ObserveGeminiOutput([]byte(`{"candidates":[{"content":{"parts":[{"inlineData":{"mimeType":"image/png","data":" "}}]}}]}`)))
}

func TestObserveImageOutput(t *testing.T) {
	require.Equal(t, StreamOutputObservation{}, ObserveImageOutput([]byte(`{"type":"image_generation.partial_image","b64_json":""}`)))
	require.Equal(t, StreamOutputObservation{}, ObserveImageOutput([]byte(`{"type":"image_generation.partial_image","b64_json":" "}`)))
	require.Equal(t, streamOutput(StreamOutputImage, false), ObserveImageOutput([]byte(`{"type":"image_generation.partial_image","b64_json":"aW1hZ2U="}`)))
	require.Equal(t, streamOutput(StreamOutputImage, false), ObserveImageOutput([]byte(`{"type":"image_edit.completed","url":"https://example.test/image.png"}`)))
}

func TestStreamOutputWireFields(t *testing.T) {
	refusal := "cannot comply"
	chunk := ChatCompletionsChunk{
		AggregateOutput: true,
		Choices: []ChatChunkChoice{{Delta: ChatDelta{
			Refusal:      &refusal,
			FunctionCall: &ChatFunctionCall{Name: "lookup", Arguments: `{"q":"docs"}`},
		}}},
	}
	encoded, err := json.Marshal(chunk)
	require.NoError(t, err)
	require.JSONEq(t, `{"id":"","object":"","created":0,"model":"","choices":[{"index":0,"delta":{"refusal":"cannot comply","function_call":{"name":"lookup","arguments":"{\"q\":\"docs\"}"}},"finish_reason":null}]}`, string(encoded))
	require.NotContains(t, string(encoded), "AggregateOutput")
	require.NotContains(t, string(encoded), "aggregate_output")

	var citationEvent AnthropicStreamEvent
	require.NoError(t, json.Unmarshal([]byte(`{"type":"content_block_delta","delta":{"type":"citations_delta","citation":{"type":"char_location","cited_text":"docs"}}}`), &citationEvent))
	require.Equal(t, streamOutput(StreamOutputText, false), ObserveAnthropicOutput(&citationEvent))
}

func TestChatCompatibilityStreamsPreserveRefusalAndLegacyFunctionCall(t *testing.T) {
	refusal := "cannot comply"
	legacyCall := &ChatFunctionCall{Name: "lookup", Arguments: `{"q":"docs"}`}

	t.Run("responses refusal", func(t *testing.T) {
		events := ChatCompletionsChunkToResponsesEvents(
			&ChatCompletionsChunk{Choices: []ChatChunkChoice{{Delta: ChatDelta{Refusal: &refusal}}}},
			NewChatCompletionsToResponsesStreamState("model"),
		)
		require.True(t, responsesEventsContain(events, "response.output_text.delta", refusal))
	})

	t.Run("responses legacy function call", func(t *testing.T) {
		events := ChatCompletionsChunkToResponsesEvents(
			&ChatCompletionsChunk{Choices: []ChatChunkChoice{{Delta: ChatDelta{FunctionCall: legacyCall}}}},
			NewChatCompletionsToResponsesStreamState("model"),
		)
		require.True(t, responsesEventsContain(events, "response.function_call_arguments.delta", legacyCall.Arguments))
	})

	t.Run("anthropic refusal", func(t *testing.T) {
		events := ChatCompletionsChunkToAnthropicEvents(
			&ChatCompletionsChunk{Choices: []ChatChunkChoice{{Delta: ChatDelta{Refusal: &refusal}}}},
			NewChatCompletionsToAnthropicStreamState("model"),
		)
		require.True(t, anthropicEventsContain(events, "text_delta", refusal))
	})

	t.Run("anthropic legacy function call", func(t *testing.T) {
		events := ChatCompletionsChunkToAnthropicEvents(
			&ChatCompletionsChunk{Choices: []ChatChunkChoice{{Delta: ChatDelta{FunctionCall: legacyCall}}}},
			NewChatCompletionsToAnthropicStreamState("model"),
		)
		require.True(t, anthropicEventsContain(events, "input_json_delta", legacyCall.Arguments))
	})
}

func responsesEventsContain(events []ResponsesStreamEvent, eventType, value string) bool {
	for _, event := range events {
		if event.Type == eventType && (event.Delta == value || event.Arguments == value || event.Input == value) {
			return true
		}
	}
	return false
}

func anthropicEventsContain(events []AnthropicStreamEvent, deltaType, value string) bool {
	for _, event := range events {
		if event.Delta == nil || event.Delta.Type != deltaType {
			continue
		}
		if event.Delta.Text == value || event.Delta.PartialJSON == value {
			return true
		}
	}
	return false
}
