package apicompat

import (
	"encoding/json"
	"strings"
)

// StreamOutputKind describes the first downstream-consumable output modality.
type StreamOutputKind string

const (
	StreamOutputNone      StreamOutputKind = ""
	StreamOutputText      StreamOutputKind = "text"
	StreamOutputReasoning StreamOutputKind = "reasoning"
	StreamOutputTool      StreamOutputKind = "tool"
	StreamOutputImage     StreamOutputKind = "image"
	StreamOutputAudio     StreamOutputKind = "audio"
)

// StreamOutputObservation separates any meaningful output from token-like
// text/reasoning/tool deltas. Image and audio output are meaningful, but never
// populate first_token_ms.
type StreamOutputObservation struct {
	MeaningfulOutput bool
	TokenLikeDelta   bool
	Kind             StreamOutputKind
}

func streamOutput(kind StreamOutputKind, tokenLike bool) StreamOutputObservation {
	return StreamOutputObservation{MeaningfulOutput: true, TokenLikeDelta: tokenLike, Kind: kind}
}

// ObserveChatChunkOutput classifies the actual Chat Completions chunk sent to
// the downstream client. Role-only, finish-only, usage-only and empty chunks do
// not count. Whitespace content is intentionally retained as a real token.
func ObserveChatChunkOutput(chunk *ChatCompletionsChunk) StreamOutputObservation {
	if chunk == nil {
		return StreamOutputObservation{}
	}
	tokenLike := !chunk.AggregateOutput
	for _, choice := range chunk.Choices {
		if choice.Delta.Content != nil && len(*choice.Delta.Content) > 0 {
			return streamOutput(StreamOutputText, tokenLike)
		}
		if choice.Delta.Refusal != nil && len(*choice.Delta.Refusal) > 0 {
			return streamOutput(StreamOutputText, tokenLike)
		}
		if choice.Delta.ReasoningContent != nil && len(*choice.Delta.ReasoningContent) > 0 {
			return streamOutput(StreamOutputReasoning, tokenLike)
		}
		if call := choice.Delta.FunctionCall; call != nil && (len(call.Name) > 0 || len(call.Arguments) > 0) {
			return streamOutput(StreamOutputTool, tokenLike)
		}
		for _, call := range choice.Delta.ToolCalls {
			if len(call.Function.Name) > 0 || len(call.Function.Arguments) > 0 {
				return streamOutput(StreamOutputTool, tokenLike)
			}
		}
	}
	return StreamOutputObservation{}
}

// ObserveAnthropicOutput classifies a downstream Anthropic SSE event.
func ObserveAnthropicOutput(event *AnthropicStreamEvent) StreamOutputObservation {
	if event == nil {
		return StreamOutputObservation{}
	}
	switch event.Type {
	case "content_block_delta":
		if event.Delta == nil {
			return StreamOutputObservation{}
		}
		if len(event.Delta.Text) > 0 {
			return streamOutput(StreamOutputText, true)
		}
		if len(event.Delta.Thinking) > 0 {
			return streamOutput(StreamOutputReasoning, true)
		}
		if len(event.Delta.Signature) > 0 {
			return streamOutput(StreamOutputReasoning, false)
		}
		if hasSemanticJSON(event.Delta.Citation) {
			return streamOutput(StreamOutputText, false)
		}
		if len(event.Delta.PartialJSON) > 0 {
			return streamOutput(StreamOutputTool, true)
		}
	case "content_block_start":
		block := event.ContentBlock
		if block == nil {
			return StreamOutputObservation{}
		}
		switch block.Type {
		case "text":
			if len(block.Text) > 0 {
				return streamOutput(StreamOutputText, true)
			}
		case "thinking":
			if len(block.Thinking) > 0 {
				return streamOutput(StreamOutputReasoning, true)
			}
			if len(block.Signature) > 0 {
				return streamOutput(StreamOutputReasoning, false)
			}
		case "redacted_thinking":
			if len(block.Data) > 0 {
				return streamOutput(StreamOutputReasoning, false)
			}
		case "tool_use", "server_tool_use":
			if len(block.Name) > 0 || hasNonEmptyJSON(block.Input) {
				return streamOutput(StreamOutputTool, true)
			}
		case "tool_result", "web_search_tool_result", "web_fetch_tool_result",
			"code_execution_tool_result", "bash_code_execution_tool_result",
			"text_editor_code_execution_tool_result":
			if hasNonEmptyJSON(block.Content) {
				return streamOutput(StreamOutputTool, false)
			}
		case "image":
			if block.Source != nil && len(strings.TrimSpace(block.Source.Data)) > 0 {
				return streamOutput(StreamOutputImage, false)
			}
		}
	}
	return StreamOutputObservation{}
}

// ObserveResponsesOutput classifies one Responses-shaped JSON event. Delta
// events may be token-like; aggregate output found only in an item-done or
// terminal response is first output but not first token.
func ObserveResponsesOutput(payload []byte) StreamOutputObservation {
	root, ok := decodeJSONObject(payload)
	if !ok {
		return StreamOutputObservation{}
	}
	eventType, _ := root["type"].(string)

	switch eventType {
	case "response.output_text.delta", "response.refusal.delta":
		if nonEmptyString(root["delta"]) {
			return streamOutput(StreamOutputText, true)
		}
		return StreamOutputObservation{}
	case "response.output_text.done", "response.refusal.done":
		if nonEmptyString(root["text"]) || nonEmptyString(root["refusal"]) {
			return streamOutput(StreamOutputText, false)
		}
		return StreamOutputObservation{}
	case "response.reasoning_summary_text.delta", "response.reasoning_text.delta":
		if nonEmptyString(root["delta"]) {
			return streamOutput(StreamOutputReasoning, true)
		}
		return StreamOutputObservation{}
	case "response.reasoning_summary_text.done", "response.reasoning_text.done":
		if nonEmptyString(root["text"]) {
			return streamOutput(StreamOutputReasoning, false)
		}
		return StreamOutputObservation{}
	case "response.output_text.annotation.added":
		if hasSemanticMetadata(root["annotation"]) {
			return streamOutput(StreamOutputText, false)
		}
		return StreamOutputObservation{}
	case "response.function_call_arguments.delta",
		"response.custom_tool_call_input.delta",
		"response.tool_search_call_arguments.delta",
		"response.mcp_call_arguments.delta",
		"response.code_interpreter_call_code.delta":
		if nonEmptyString(root["delta"]) || hasNonEmptyValue(root["arguments"]) || hasNonEmptyValue(root["input"]) {
			return streamOutput(StreamOutputTool, true)
		}
		return StreamOutputObservation{}
	case "response.function_call_arguments.done",
		"response.custom_tool_call_input.done",
		"response.tool_search_call_arguments.done",
		"response.mcp_call_arguments.done",
		"response.code_interpreter_call_code.done":
		if hasNonEmptyValue(root["arguments"]) || hasNonEmptyValue(root["input"]) || nonEmptyString(root["name"]) || nonEmptyString(root["code"]) {
			return streamOutput(StreamOutputTool, false)
		}
		return StreamOutputObservation{}
	}

	if strings.Contains(eventType, "image") {
		if hasImagePayload(root) {
			return streamOutput(StreamOutputImage, false)
		}
		return StreamOutputObservation{}
	}
	if strings.Contains(eventType, "transcript") {
		if hasMediaPayload(root, "delta", "transcript", "text") {
			return streamOutput(StreamOutputText, strings.HasSuffix(eventType, ".delta"))
		}
		return StreamOutputObservation{}
	}
	if strings.Contains(eventType, "audio") {
		if hasMediaPayload(root, "delta", "audio", "audio_b64", "data") {
			return streamOutput(StreamOutputAudio, false)
		}
		return StreamOutputObservation{}
	}

	switch eventType {
	case "response.output_item.added":
		if item, ok := root["item"].(map[string]any); ok {
			if obs := observeOutputNode(item, true); obs.MeaningfulOutput {
				return obs
			}
		}
	case "response.output_item.done",
		"response.content_part.done", "response.content_part.added",
		"response.reasoning_summary_part.done", "response.reasoning_summary_part.added":
		for _, key := range []string{"item", "part"} {
			if node, ok := root[key].(map[string]any); ok {
				if obs := observeOutputNode(node, false); obs.MeaningfulOutput {
					return obs
				}
			}
		}
	case "response.completed", "response.done", "response.incomplete":
		if response, ok := root["response"].(map[string]any); ok {
			return observeOutputNode(response, false)
		}
		return observeOutputNode(root, false)
	}
	return StreamOutputObservation{}
}

// ObserveImageOutput classifies direct Images API events as well as
// Responses-shaped image events. A non-empty partial or final image counts as
// first output and never as first token.
func ObserveImageOutput(payload []byte) StreamOutputObservation {
	root, ok := decodeJSONObject(payload)
	if !ok {
		return StreamOutputObservation{}
	}
	if hasImagePayload(root) {
		return streamOutput(StreamOutputImage, false)
	}
	if obs := ObserveResponsesOutput(payload); obs.Kind == StreamOutputImage {
		return obs
	}
	return StreamOutputObservation{}
}

// ObserveGeminiOutput classifies Gemini candidate parts. Candidate metadata,
// finish reasons and usage without content do not count.
func ObserveGeminiOutput(payload []byte) StreamOutputObservation {
	root, ok := decodeJSONObject(payload)
	if !ok {
		return StreamOutputObservation{}
	}
	if response, ok := root["response"].(map[string]any); ok {
		root = response
	}
	candidates, _ := root["candidates"].([]any)
	for _, candidateRaw := range candidates {
		candidate, _ := candidateRaw.(map[string]any)
		content, _ := candidate["content"].(map[string]any)
		parts, _ := content["parts"].([]any)
		for _, partRaw := range parts {
			part, _ := partRaw.(map[string]any)
			if nonEmptyString(part["text"]) {
				if thought, _ := part["thought"].(bool); thought {
					return streamOutput(StreamOutputReasoning, true)
				}
				return streamOutput(StreamOutputText, true)
			}
			if call, ok := part["functionCall"].(map[string]any); ok {
				if nonEmptyString(call["name"]) || hasNonEmptyValue(call["args"]) {
					return streamOutput(StreamOutputTool, true)
				}
			}
			if call, ok := part["function_call"].(map[string]any); ok {
				if nonEmptyString(call["name"]) || hasNonEmptyValue(call["args"]) {
					return streamOutput(StreamOutputTool, true)
				}
			}
			for _, key := range []string{"executableCode", "executable_code"} {
				if code, ok := part[key].(map[string]any); ok && nonEmptyString(code["code"]) {
					return streamOutput(StreamOutputTool, true)
				}
			}
			for _, key := range []string{"codeExecutionResult", "code_execution_result"} {
				if result, ok := part[key].(map[string]any); ok && hasNonEmptyValue(result["output"]) {
					return streamOutput(StreamOutputTool, false)
				}
			}
			for _, key := range []string{"inlineData", "inline_data"} {
				if media, ok := part[key].(map[string]any); ok && nonBlankString(media["data"]) {
					if obs := observeGeminiMedia(media); obs.MeaningfulOutput {
						return obs
					}
				}
			}
			for _, key := range []string{"fileData", "file_data"} {
				if media, ok := part[key].(map[string]any); ok &&
					(nonBlankString(media["fileUri"]) || nonBlankString(media["file_uri"])) {
					if obs := observeGeminiMedia(media); obs.MeaningfulOutput {
						return obs
					}
				}
			}
			if nonBlankString(part["thoughtSignature"]) || nonBlankString(part["thought_signature"]) {
				return streamOutput(StreamOutputReasoning, false)
			}
		}
		for _, key := range []string{"groundingMetadata", "grounding_metadata", "citationMetadata", "citation_metadata"} {
			if hasSemanticMetadata(candidate[key]) {
				return streamOutput(StreamOutputText, false)
			}
		}
	}
	return StreamOutputObservation{}
}

func observeGeminiMedia(media map[string]any) StreamOutputObservation {
	mime := streamStringValue(media["mimeType"])
	if mime == "" {
		mime = streamStringValue(media["mime_type"])
	}
	mime = strings.ToLower(strings.TrimSpace(mime))
	if strings.HasPrefix(mime, "audio/") {
		return streamOutput(StreamOutputAudio, false)
	}
	if strings.HasPrefix(mime, "image/") {
		return streamOutput(StreamOutputImage, false)
	}
	return StreamOutputObservation{}
}

func observeOutputNode(node map[string]any, tokenLike bool) StreamOutputObservation {
	kind := strings.ToLower(streamStringValue(node["type"]))
	switch {
	case strings.Contains(kind, "image"):
		if hasImagePayload(node) {
			return streamOutput(StreamOutputImage, false)
		}
	case strings.Contains(kind, "transcript"):
		if hasMediaPayload(node, "transcript", "text", "delta") {
			return streamOutput(StreamOutputText, tokenLike)
		}
	case strings.Contains(kind, "audio"):
		if hasMediaPayload(node, "audio", "audio_b64", "data") {
			return streamOutput(StreamOutputAudio, false)
		}
		if hasMediaPayload(node, "transcript", "text") {
			return streamOutput(StreamOutputText, tokenLike)
		}
	case kind == "output_text" || kind == "text":
		if nonEmptyString(node["text"]) || nonEmptyString(node["delta"]) {
			return streamOutput(StreamOutputText, tokenLike)
		}
		if hasSemanticMetadata(node["annotations"]) {
			return streamOutput(StreamOutputText, false)
		}
	case kind == "refusal":
		if nonEmptyString(node["refusal"]) || nonEmptyString(node["text"]) {
			return streamOutput(StreamOutputText, tokenLike)
		}
	case kind == "summary_text":
		if nonEmptyString(node["text"]) {
			return streamOutput(StreamOutputReasoning, tokenLike)
		}
	case kind == "reasoning" || strings.Contains(kind, "reasoning") || strings.Contains(kind, "thinking"):
		if nonEmptyString(node["text"]) || nonEmptyString(node["delta"]) || nonEmptyString(node["summary"]) {
			return streamOutput(StreamOutputReasoning, tokenLike)
		}
		if nonBlankString(node["encrypted_content"]) || nonBlankString(node["signature"]) || nonBlankString(node["data"]) {
			return streamOutput(StreamOutputReasoning, false)
		}
	case isResponsesToolOutputKind(kind):
		if hasToolPayload(node) {
			return streamOutput(StreamOutputTool, tokenLike)
		}
	}

	for _, key := range []string{"output", "content", "summary", "result"} {
		switch value := node[key].(type) {
		case []any:
			for _, childRaw := range value {
				if child, ok := childRaw.(map[string]any); ok {
					if obs := observeOutputNode(child, tokenLike); obs.MeaningfulOutput {
						return obs
					}
				}
			}
		case map[string]any:
			if obs := observeOutputNode(value, tokenLike); obs.MeaningfulOutput {
				return obs
			}
		}
	}
	return StreamOutputObservation{}
}

func hasImagePayload(node map[string]any) bool {
	for _, key := range []string{"partial_image_b64", "partial_image", "b64_json", "url", "image_url"} {
		if nonBlankString(node[key]) {
			return true
		}
	}
	kind := strings.ToLower(streamStringValue(node["type"]))
	if strings.Contains(kind, "image") && nonBlankString(node["result"]) {
		return true
	}
	for _, key := range []string{"item", "response", "output", "data", "result", "content"} {
		switch value := node[key].(type) {
		case map[string]any:
			if hasImagePayload(value) {
				return true
			}
		case []any:
			for _, childRaw := range value {
				if child, ok := childRaw.(map[string]any); ok && hasImagePayload(child) {
					return true
				}
			}
		}
	}
	return false
}

func hasMediaPayload(node map[string]any, keys ...string) bool {
	for _, key := range keys {
		if nonBlankString(node[key]) {
			return true
		}
	}
	return false
}

func hasToolPayload(node map[string]any) bool {
	for _, key := range []string{
		"name", "arguments", "input", "action", "command", "code",
		"query", "queries", "results", "output", "tools", "operation",
		"patch", "diff", "stdout", "stderr", "logs",
	} {
		if hasNonEmptyValue(node[key]) {
			return true
		}
	}
	for _, key := range []string{"function", "tool_calls"} {
		switch value := node[key].(type) {
		case map[string]any:
			if hasToolPayload(value) {
				return true
			}
		case []any:
			for _, childRaw := range value {
				if child, ok := childRaw.(map[string]any); ok && hasToolPayload(child) {
					return true
				}
			}
		}
	}
	return false
}

func isResponsesToolOutputKind(kind string) bool {
	kind = strings.ToLower(strings.TrimSpace(kind))
	if strings.HasSuffix(kind, "_call") || strings.HasSuffix(kind, "_call_output") ||
		strings.Contains(kind, "function_call") || strings.Contains(kind, "tool_call") {
		return true
	}
	switch kind {
	case "tool_search_output", "mcp_list_tools", "mcp_approval_request", "mcp_approval_response":
		return true
	default:
		return false
	}
}

func decodeJSONObject(payload []byte) (map[string]any, bool) {
	var root map[string]any
	if len(payload) == 0 || json.Unmarshal(payload, &root) != nil || root == nil {
		return nil, false
	}
	return root, true
}

func hasNonEmptyJSON(raw json.RawMessage) bool {
	trimmed := strings.TrimSpace(string(raw))
	return trimmed != "" && trimmed != "null" && trimmed != "{}"
}

func hasSemanticJSON(raw json.RawMessage) bool {
	var value any
	if len(raw) == 0 || json.Unmarshal(raw, &value) != nil {
		return false
	}
	return hasSemanticMetadata(value)
}

func hasNonEmptyValue(value any) bool {
	switch v := value.(type) {
	case nil:
		return false
	case string:
		return len(v) > 0
	case []any:
		return len(v) > 0
	case map[string]any:
		return len(v) > 0
	default:
		return true
	}
}

func hasSemanticMetadata(value any) bool {
	switch v := value.(type) {
	case nil:
		return false
	case string:
		return len(v) > 0
	case bool:
		return v
	case []any:
		for _, child := range v {
			if hasSemanticMetadata(child) {
				return true
			}
		}
		return false
	case map[string]any:
		for _, child := range v {
			if hasSemanticMetadata(child) {
				return true
			}
		}
		return false
	default:
		return true
	}
}

func nonEmptyString(value any) bool {
	text, ok := value.(string)
	return ok && len(text) > 0
}

func nonBlankString(value any) bool {
	text, ok := value.(string)
	return ok && strings.TrimSpace(text) != ""
}

func streamStringValue(value any) string {
	text, _ := value.(string)
	return text
}
