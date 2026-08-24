package auditcontent

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
)

var ErrIncompleteContent = errors.New("audit content extraction is incomplete")

type Source string

const (
	SourceMessage        Source = "message"
	SourceInstruction    Source = "instruction"
	SourceToolCall       Source = "tool_call"
	SourceToolDefinition Source = "tool_definition"
	SourceToolOutput     Source = "tool_output"
	SourceSearchQuery    Source = "search_query"
	SourceEmbeddingInput Source = "embedding_input"
	SourceMediaPrompt    Source = "media_prompt"
	SourcePromptVariable Source = "prompt_variable"
	SourceReasoning      Source = "reasoning"
)

type Segment struct {
	Text             string
	Role             string
	Source           Source
	Current          bool
	ClientControlled bool
}

type Image struct {
	URL              string
	Role             string
	Source           Source
	Current          bool
	ClientControlled bool
}

type Document struct {
	Segments       []Segment
	Images         []Image
	ContentBearing bool
	Incomplete     bool
}

func Extract(protocol string, body []byte) (Document, error) {
	if len(body) == 0 {
		return Document{}, nil
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	var decoded any
	if err := decoder.Decode(&decoded); err != nil {
		return Document{}, err
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return Document{}, errors.New("audit content request contains multiple JSON values")
		}
		return Document{}, err
	}
	root, ok := decoded.(map[string]any)
	if !ok {
		return Document{}, errors.New("audit content request root must be an object")
	}

	var document Document
	switch normalizeProtocol(protocol) {
	case "openai_chat_completions", "openai_chat", "chat_completions":
		extractChat(&document, root)
	case "anthropic_messages", "claude_messages", "messages":
		extractAnthropic(&document, root)
	case "gemini", "gemini_generate_content":
		extractGeminiRoot(&document, root)
	case "openai_responses", "responses", "responses_websocket":
		extractResponsesRoot(&document, root)
	case "openai_live", "live":
		extractLiveRoot(&document, root)
	case "openai_alpha_search", "alpha_search":
		extractAlphaSearch(&document, root)
	case "openai_embeddings", "embeddings":
		extractEmbeddings(&document, root)
	case "openai_images", "grok_media", "media", "images":
		extractMediaPrompts(&document, root)
	default:
		extractDefault(&document, root)
	}
	normalizeDocument(&document)
	return document, nil
}

func normalizeProtocol(protocol string) string {
	return strings.ToLower(strings.TrimSpace(protocol))
}

func normalizeDocument(document *Document) {
	if document == nil {
		return
	}
	out := make([]Segment, 0, len(document.Segments))
	for _, segment := range document.Segments {
		segment.Text = strings.TrimSpace(segment.Text)
		segment.Role = strings.ToLower(strings.TrimSpace(segment.Role))
		if segment.Text != "" {
			out = append(out, segment)
		}
	}
	document.Segments = out
	images := make([]Image, 0, len(document.Images))
	seenImages := make(map[string]struct{}, len(document.Images))
	for _, image := range document.Images {
		image.URL = strings.TrimSpace(image.URL)
		image.Role = strings.ToLower(strings.TrimSpace(image.Role))
		key := image.URL + "\x00" + image.Role + "\x00" + string(image.Source) + fmt.Sprintf("\x00%t\x00%t", image.Current, image.ClientControlled)
		if image.URL != "" {
			if _, duplicate := seenImages[key]; duplicate {
				continue
			}
			seenImages[key] = struct{}{}
			images = append(images, image)
		}
	}
	document.Images = images
}

func extractDefault(document *Document, root map[string]any) {
	if _, ok := root["messages"]; ok {
		extractChat(document, root)
	}
	if _, input := root["input"]; input {
		extractResponsesRoot(document, root)
	} else if _, instructions := root["instructions"]; instructions {
		extractResponsesRoot(document, root)
	}
	if hasAnyKey(root, "contents", "content", "systemInstruction", "system_instruction", "instances", "requests") {
		extractGeminiRoot(document, root)
	}
	if _, ok := root["commands"]; ok {
		extractAlphaSearch(document, root)
	}
	if len(document.Segments) == 0 {
		extractMediaPrompts(document, root)
	}
}

func extractChat(document *Document, root map[string]any) {
	if instructions, exists := root["instructions"]; exists {
		markContentBearing(document, instructions)
		appendContent(document, instructions, "system", SourceInstruction, true, true)
	}
	appendToolDefinitions(document, root["tools"])
	appendToolDefinitions(document, root["functions"])
	messagesValue, messagesExist := root["messages"]
	messages, ok := asSlice(messagesValue)
	if !ok {
		if messagesExist && hasNonEmptyValue(messagesValue) {
			markIncompleteContent(document)
		}
		return
	}
	if len(messages) == 0 {
		return
	}
	currentStart := len(messages) - 1
	if isChatToolOutput(messages[currentStart]) {
		for currentStart > 0 && isChatToolOutput(messages[currentStart-1]) {
			currentStart--
		}
	}
	for index, item := range messages {
		message, ok := item.(map[string]any)
		if !ok {
			if hasNonEmptyValue(item) {
				markIncompleteContent(document)
			}
			continue
		}
		role := normalizedRole(message["role"])
		current := index >= currentStart
		if content, exists := message["content"]; exists {
			markContentBearing(document, content)
		}
		if hasNonEmptyValue(message["tool_calls"]) || hasNonEmptyValue(message["function_call"]) {
			document.ContentBearing = true
		}
		source := SourceMessage
		controlled := true
		switch role {
		case "system", "developer":
			source = SourceInstruction
			current = true
		case "tool", "function":
			source = SourceToolOutput
		}
		if source == SourceToolOutput {
			appendToolOutput(document, message["content"], current)
		} else {
			appendContent(document, message["content"], role, source, current, controlled)
		}
		if refusal, exists := message["refusal"]; exists {
			markContentBearing(document, refusal)
			appendStructured(document, refusal, "assistant", SourceMessage, current, controlled)
		}
		appendChatToolCalls(document, message, role, current)
		markUnknownNonEmptyFields(document, message, "role", "content", "name", "tool_call_id", "tool_calls", "function_call", "refusal", "audio")
	}
}

func isChatToolOutput(value any) bool {
	message, ok := value.(map[string]any)
	if !ok {
		return false
	}
	role := normalizedRole(message["role"])
	return role == "tool" || role == "function"
}

func appendChatToolCalls(document *Document, message map[string]any, role string, current bool) {
	if callsValue, exists := message["tool_calls"]; exists && hasNonEmptyValue(callsValue) {
		calls, ok := asSlice(callsValue)
		if !ok {
			markIncompleteContent(document)
		} else {
			for _, value := range calls {
				call, ok := value.(map[string]any)
				if !ok {
					markIncompleteContent(document)
					continue
				}
				function, _ := call["function"].(map[string]any)
				if arguments, exists := function["arguments"]; exists {
					appendStructured(document, arguments, role, SourceToolCall, current, true)
				} else if hasNonEmptyValue(call) {
					markIncompleteContent(document)
				}
				markUnknownNonEmptyFields(document, call, "id", "type", "function")
				markUnknownNonEmptyFields(document, function, "name", "arguments")
			}
		}
	}
	if callValue, exists := message["function_call"]; exists && hasNonEmptyValue(callValue) {
		call, ok := callValue.(map[string]any)
		if !ok {
			markIncompleteContent(document)
			return
		}
		if arguments, exists := call["arguments"]; exists {
			appendStructured(document, arguments, role, SourceToolCall, current, true)
		} else {
			markIncompleteContent(document)
		}
		markUnknownNonEmptyFields(document, call, "name", "arguments")
	}
}

func extractAnthropic(document *Document, root map[string]any) {
	if system, exists := root["system"]; exists {
		markContentBearing(document, system)
		appendContent(document, system, "system", SourceInstruction, true, true)
	}
	appendToolDefinitions(document, root["tools"])
	messagesValue, messagesExist := root["messages"]
	messages, ok := asSlice(messagesValue)
	if !ok {
		if messagesExist && hasNonEmptyValue(messagesValue) {
			markIncompleteContent(document)
		}
		return
	}
	if len(messages) == 0 {
		return
	}
	for index, item := range messages {
		message, ok := item.(map[string]any)
		if !ok {
			if hasNonEmptyValue(item) {
				markIncompleteContent(document)
			}
			continue
		}
		role := normalizedRole(message["role"])
		if content, exists := message["content"]; exists {
			markContentBearing(document, content)
		}
		appendAnthropicContent(document, message["content"], role, index == len(messages)-1)
		markUnknownNonEmptyFields(document, message, "role", "content")
	}
}

func appendAnthropicContent(document *Document, value any, role string, current bool) {
	switch typed := value.(type) {
	case string:
		appendText(document, typed, role, SourceMessage, current, true)
	case []any:
		for _, item := range typed {
			appendAnthropicContent(document, item, role, current)
		}
	case map[string]any:
		before := len(document.Segments)
		typeName := normalizedType(typed["type"])
		switch {
		case typeName == "tool_result" || strings.HasSuffix(typeName, "_tool_result"):
			content, exists := typed["content"]
			if !exists {
				markIncompleteContent(document)
				return
			}
			appendToolOutput(document, content, current)
			markUnknownNonEmptyFields(document, typed, "type", "tool_use_id", "content", "is_error", "cache_control")
		case typeName == "tool_use" || strings.HasSuffix(typeName, "_tool_use"):
			input, exists := typed["input"]
			if !exists {
				markIncompleteContent(document)
				return
			}
			appendStructured(document, input, role, SourceToolCall, current, true)
			markUnknownNonEmptyFields(document, typed, "type", "id", "name", "input", "cache_control")
		case typeName == "thinking":
			thinking, exists := typed["thinking"]
			if !exists {
				markIncompleteContent(document)
				return
			}
			appendStructured(document, thinking, "assistant", SourceReasoning, current, true)
			markUnknownNonEmptyFields(document, typed, "type", "thinking", "signature")
		case typeName == "redacted_thinking":
			markUnknownNonEmptyFields(document, typed, "type", "data")
			return
		case typeName == "image" || typeName == "image_url" || typeName == "input_image":
			appendImageValues(document, typed, role, SourceMessage, current, true, true)
			if text, exists := typed["text"]; exists {
				markContentBearing(document, text)
				appendStructured(document, text, role, SourceMessage, current, true)
			}
			if content, exists := typed["content"]; exists {
				markContentBearing(document, content)
				appendAnthropicContent(document, content, role, current)
			}
			markUnknownNonEmptyFields(document, typed, "type", "source", "image_url", "url", "text", "content", "cache_control")
			return
		default:
			contentRole := role
			if typeName == "output_text" {
				contentRole = "assistant"
			}
			appendImageValues(document, typed, contentRole, SourceMessage, current, true, false)
			if text, ok := typed["text"].(string); ok {
				appendText(document, text, contentRole, SourceMessage, current, true)
			}
			if content, exists := typed["content"]; exists {
				appendAnthropicContent(document, content, contentRole, current)
			}
			if typeName == "" || typeName == "text" || typeName == "input_text" || typeName == "output_text" {
				markUnknownNonEmptyFields(document, typed, "type", "text", "content", "citations", "cache_control")
			} else {
				markIncompleteContent(document)
			}
		}
		if len(document.Segments) == before && contentRequiresExtraction(typed) {
			markIncompleteContent(document)
		}
	default:
		if hasNonEmptyValue(typed) {
			markIncompleteContent(document)
		}
	}
}

func appendToolOutput(document *Document, value any, current bool) {
	if !hasNonEmptyValue(value) {
		return
	}
	appendStructured(document, value, "tool", SourceToolOutput, current, true)
}

func extractResponsesRoot(document *Document, root map[string]any) {
	typeName := normalizedType(root["type"])
	switch typeName {
	case "conversation.item.create":
		item, ok := root["item"].(map[string]any)
		if !ok {
			markIncompleteContent(document)
		} else {
			appendResponsesItem(document, item, true)
		}
		markUnknownNonEmptyFields(document, root, "type", "event_id", "item", "previous_item_id")
	case "session.update":
		session, ok := root["session"].(map[string]any)
		if !ok {
			markIncompleteContent(document)
		} else {
			extractResponsesSession(document, session)
		}
		markUnknownNonEmptyFields(document, root, "type", "event_id", "session")
	case "response.cancel":
		markUnknownNonEmptyFields(document, root, "type", "event_id", "response_id")
	case "conversation.item.retrieve", "conversation.item.delete":
		markUnknownNonEmptyFields(document, root, "type", "event_id", "item_id")
	case "conversation.item.truncate":
		markUnknownNonEmptyFields(document, root, "type", "event_id", "item_id", "content_index", "audio_end_ms")
	case "input_audio_buffer.append":
		markUnknownNonEmptyFields(document, root, "type", "event_id", "audio")
	case "input_audio_buffer.commit", "input_audio_buffer.clear", "session.close":
		markUnknownNonEmptyFields(document, root, "type", "event_id")
	case "output_audio_buffer.clear":
		markUnknownNonEmptyFields(document, root, "type", "event_id", "response_id")
	case "", "response.create":
		markUnknownResponsesRequestFields(document, root, true)
	default:
		if !hasAnyKey(root, "instructions", "tools", "prompt", "input", "response") {
			markIncompleteContent(document)
		} else {
			markUnknownResponsesRequestFields(document, root, true)
		}
	}
	if instructions, exists := root["instructions"]; exists {
		markContentBearing(document, instructions)
		appendContent(document, instructions, "system", SourceInstruction, true, true)
	}
	appendToolDefinitions(document, root["tools"])
	appendResponsesPrompt(document, root["prompt"])
	if input, exists := root["input"]; exists {
		extractResponsesValue(document, input)
	}
	responseValue, responseExists := root["response"]
	if !responseExists {
		return
	}
	response, ok := responseValue.(map[string]any)
	if !ok {
		if hasNonEmptyValue(responseValue) {
			markIncompleteContent(document)
		}
		return
	}
	if instructions, exists := response["instructions"]; exists {
		markContentBearing(document, instructions)
		appendContent(document, instructions, "system", SourceInstruction, true, true)
	}
	appendToolDefinitions(document, response["tools"])
	appendResponsesPrompt(document, response["prompt"])
	if input, exists := response["input"]; exists {
		extractResponsesValue(document, input)
	}
	markUnknownResponsesRequestFields(document, response, false)
}

func extractResponsesSession(document *Document, session map[string]any) {
	if instructions, exists := session["instructions"]; exists {
		markContentBearing(document, instructions)
		appendContent(document, instructions, "system", SourceInstruction, true, true)
	}
	appendToolDefinitions(document, session["tools"])
	appendResponsesPrompt(document, session["prompt"])
	if input, exists := session["input"]; exists {
		extractResponsesValue(document, input)
	}
	markUnknownResponsesRequestFields(document, session, false)
}

func markUnknownResponsesRequestFields(document *Document, values map[string]any, envelope bool) {
	allowed := []string{
		"background", "context_management", "conversation", "include", "input", "instructions",
		"max_output_tokens", "max_tool_calls", "metadata", "model", "parallel_tool_calls",
		"previous_response_id", "prompt", "prompt_cache_key", "prompt_cache_options",
		"prompt_cache_retention", "reasoning", "reasoning_effort", "safety_identifier",
		"service_tier", "store", "stream", "stream_options", "temperature", "text",
		"tool_choice", "tools", "top_logprobs", "top_p", "truncation", "user",
	}
	if envelope {
		allowed = append(allowed, "type", "event_id", "response")
	}
	markUnknownNonEmptyFields(document, values, allowed...)
}

func appendResponsesPrompt(document *Document, value any) {
	if !hasNonEmptyValue(value) {
		return
	}
	prompt, ok := value.(map[string]any)
	if !ok {
		markIncompleteContent(document)
		return
	}
	markUnknownNonEmptyFields(document, prompt, "id", "version", "variables")
	variablesValue, exists := prompt["variables"]
	if !exists || !hasNonEmptyValue(variablesValue) {
		return
	}
	variables, ok := variablesValue.(map[string]any)
	if !ok {
		markIncompleteContent(document)
		return
	}
	keys := make([]string, 0, len(variables))
	for key := range variables {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		value := variables[key]
		if !hasNonEmptyValue(value) {
			continue
		}
		document.ContentBearing = true
		switch typed := value.(type) {
		case string:
			appendText(document, typed, "user", SourcePromptVariable, true, true)
		case map[string]any:
			typeName := normalizedType(typed["type"])
			switch typeName {
			case "text", "input_text":
				text, exists := typed["text"]
				if !exists {
					markIncompleteContent(document)
				} else {
					appendStructured(document, text, "user", SourcePromptVariable, true, true)
				}
				markUnknownNonEmptyFields(document, typed, "type", "text")
			case "image", "image_url", "input_image", "input_file", "file":
				appendImageValues(document, typed, "user", SourcePromptVariable, true, true, true)
				// Reusable prompt variables can contain media. Canonical image
				// attribution keeps it available to Prompt Audit without treating
				// it as a direct-user Content Moderation input.
				markUnknownNonEmptyFields(document, typed, "type", "image_url", "url", "file_id", "file_url", "file_data", "filename", "detail", "source", "data", "media_type", "mime_type")
			default:
				markIncompleteContent(document)
			}
		default:
			markIncompleteContent(document)
		}
	}
}

func extractResponsesValue(document *Document, value any) {
	switch typed := value.(type) {
	case string:
		markContentBearing(document, typed)
		appendText(document, typed, "user", SourceMessage, true, true)
	case []any:
		if len(typed) == 0 {
			return
		}
		currentStart := len(typed) - 1
		if isResponsesToolOutput(typed[currentStart]) {
			for currentStart > 0 && isResponsesToolOutput(typed[currentStart-1]) {
				currentStart--
			}
		}
		for index, item := range typed {
			appendResponsesItem(document, item, index >= currentStart)
		}
	case map[string]any:
		appendResponsesItem(document, typed, true)
	default:
		if hasNonEmptyValue(typed) {
			markIncompleteContent(document)
		}
	}
}

func appendResponsesItem(document *Document, value any, current bool) {
	switch typed := value.(type) {
	case string:
		markContentBearing(document, typed)
		appendText(document, typed, "user", SourceMessage, current, true)
	case map[string]any:
		typeName := normalizedType(typed["type"])
		role := normalizedRole(typed["role"])
		switch typeName {
		case "function_call_output", "custom_tool_call_output", "local_shell_call_output",
			"shell_call_output", "apply_patch_call_output", "mcp_tool_call_output":
			output, exists := typed["output"]
			if !exists {
				markIncompleteContent(document)
			} else {
				markContentBearing(document, output)
				appendToolOutput(document, output, current)
			}
			markUnknownNonEmptyFields(document, typed, "type", "id", "call_id", "output", "status")
			return
		case "computer_call_output":
			output, exists := typed["output"]
			if !exists {
				markIncompleteContent(document)
			} else {
				markContentBearing(document, output)
				appendToolOutput(document, output, current)
			}
			if checks, exists := typed["acknowledged_safety_checks"]; exists && hasNonEmptyValue(checks) {
				markContentBearing(document, checks)
				appendToolOutput(document, checks, current)
			}
			markUnknownNonEmptyFields(document, typed, "type", "id", "call_id", "output", "status", "acknowledged_safety_checks")
			return
		case "tool_search_output":
			if tools, exists := typed["tools"]; exists {
				appendToolDefinitions(document, tools)
			} else if output, legacyExists := typed["output"]; legacyExists {
				// Keep accepting the pre-release output form used by older
				// compatible clients while auditing it with identical semantics.
				markContentBearing(document, output)
				appendToolOutput(document, output, current)
			} else {
				markIncompleteContent(document)
			}
			markUnknownNonEmptyFields(document, typed, "type", "id", "execution", "call_id", "status", "tools", "output")
			return
		case "function_call", "mcp_tool_call":
			arguments, exists := typed["arguments"]
			if !exists {
				markIncompleteContent(document)
			} else {
				markContentBearing(document, arguments)
				appendStructured(document, arguments, roleOr(role, "assistant"), SourceToolCall, current, true)
			}
			markUnknownNonEmptyFields(document, typed, "type", "id", "call_id", "name", "arguments", "status")
			return
		case "custom_tool_call":
			input, exists := typed["input"]
			if !exists {
				markIncompleteContent(document)
			} else {
				markContentBearing(document, input)
				appendStructured(document, input, roleOr(role, "assistant"), SourceToolCall, current, true)
			}
			markUnknownNonEmptyFields(document, typed, "type", "id", "call_id", "name", "input", "status")
			return
		case "tool_search_call":
			arguments, exists := typed["arguments"]
			if !exists {
				markIncompleteContent(document)
			} else {
				markContentBearing(document, arguments)
				appendStructured(document, arguments, roleOr(role, "assistant"), SourceToolCall, current, true)
			}
			markUnknownNonEmptyFields(document, typed, "type", "id", "call_id", "status", "arguments", "execution")
			return
		case "tool_call", "local_shell_call", "shell_call", "apply_patch_call", "computer_call":
			payload := firstExisting(typed, "arguments", "input", "action", "operation", "tool_calls")
			if payload == nil && hasNonEmptyValue(typed) {
				markIncompleteContent(document)
			} else if payload != nil {
				markContentBearing(document, payload)
				appendStructured(document, payload, roleOr(role, "assistant"), SourceToolCall, current, true)
			}
			if checks, exists := typed["pending_safety_checks"]; exists && hasNonEmptyValue(checks) {
				markContentBearing(document, checks)
				appendStructured(document, checks, roleOr(role, "assistant"), SourceToolCall, current, true)
			}
			markUnknownNonEmptyFields(document, typed, "type", "id", "call_id", "status", "name", "arguments", "input", "action", "operation", "tool_calls", "pending_safety_checks")
			return
		case "mcp_call":
			found := false
			if arguments, exists := typed["arguments"]; exists {
				found = true
				markContentBearing(document, arguments)
				appendStructured(document, arguments, roleOr(role, "assistant"), SourceToolCall, current, true)
			}
			for _, key := range []string{"output", "error"} {
				if output, exists := typed[key]; exists && hasNonEmptyValue(output) {
					found = true
					markContentBearing(document, output)
					appendToolOutput(document, output, current)
				}
			}
			if !found {
				markIncompleteContent(document)
			}
			markUnknownNonEmptyFields(document, typed, "type", "id", "status", "approval_request_id", "arguments", "error", "name", "output", "server_label")
			return
		case "mcp_list_tools":
			tools, exists := typed["tools"]
			if !exists {
				markIncompleteContent(document)
			} else {
				appendToolDefinitions(document, tools)
			}
			if output, exists := typed["error"]; exists && hasNonEmptyValue(output) {
				appendToolOutput(document, output, current)
			}
			markUnknownNonEmptyFields(document, typed, "type", "id", "status", "server_label", "tools", "error")
			return
		case "mcp_approval_request":
			arguments, exists := typed["arguments"]
			if !exists {
				markIncompleteContent(document)
			} else {
				markContentBearing(document, arguments)
				appendStructured(document, arguments, roleOr(role, "assistant"), SourceToolCall, current, true)
			}
			markUnknownNonEmptyFields(document, typed, "type", "id", "arguments", "name", "server_label")
			return
		case "mcp_approval_response":
			if reason, exists := typed["reason"]; exists {
				markContentBearing(document, reason)
				appendStructured(document, reason, roleOr(role, "user"), SourceToolOutput, current, true)
			}
			markUnknownNonEmptyFields(document, typed, "type", "id", "approval_request_id", "approve", "reason")
			return
		case "code_interpreter_call":
			found := false
			if code, exists := typed["code"]; exists {
				found = true
				markContentBearing(document, code)
				appendStructured(document, code, roleOr(role, "assistant"), SourceToolCall, current, true)
			}
			if outputs, exists := typed["outputs"]; exists && hasNonEmptyValue(outputs) {
				found = true
				markContentBearing(document, outputs)
				appendToolOutput(document, outputs, current)
			}
			if !found {
				markIncompleteContent(document)
			}
			markUnknownNonEmptyFields(document, typed, "type", "id", "status", "code", "container_id", "outputs")
			return
		case "program":
			code, exists := typed["code"]
			if !exists {
				markIncompleteContent(document)
			} else {
				markContentBearing(document, code)
				appendStructured(document, code, roleOr(role, "assistant"), SourceToolCall, current, true)
			}
			markUnknownNonEmptyFields(document, typed, "type", "id", "status", "call_id", "code", "fingerprint")
			return
		case "program_output":
			result, exists := typed["result"]
			if !exists {
				markIncompleteContent(document)
			} else {
				markContentBearing(document, result)
				appendToolOutput(document, result, current)
			}
			markUnknownNonEmptyFields(document, typed, "type", "id", "status", "call_id", "result")
			return
		case "additional_tools":
			tools, exists := typed["tools"]
			if !exists {
				markIncompleteContent(document)
			} else {
				appendToolDefinitions(document, tools)
			}
			markUnknownNonEmptyFields(document, typed, "type", "id", "role", "tools")
			return
		case "compaction":
			markUnknownNonEmptyFields(document, typed, "type", "id", "encrypted_content")
			return
		case "reasoning":
			for _, key := range []string{"summary", "content", "text"} {
				if payload, exists := typed[key]; exists {
					markContentBearing(document, payload)
					appendContent(document, payload, "assistant", SourceReasoning, current, true)
				}
			}
			markUnknownNonEmptyFields(document, typed, "type", "id", "status", "summary", "content", "text", "encrypted_content")
			return
		case "item_reference":
			markUnknownNonEmptyFields(document, typed, "type", "id")
			return
		case "web_search_call", "file_search_call", "image_generation_call":
			payload := firstExisting(typed, "arguments", "action", "query", "revised_prompt", "prompt")
			if payload != nil {
				markContentBearing(document, payload)
				appendStructured(document, payload, roleOr(role, "assistant"), SourceToolCall, current, true)
			}
			markUnknownNonEmptyFields(document, typed, "type", "id", "status", "arguments", "action", "query", "result", "revised_prompt", "prompt")
			return
		case "refusal":
			refusal, exists := typed["refusal"]
			if !exists {
				markIncompleteContent(document)
			} else {
				markContentBearing(document, refusal)
				appendStructured(document, refusal, "assistant", SourceMessage, current, true)
			}
			markUnknownNonEmptyFields(document, typed, "type", "refusal")
			return
		}
		if content, exists := typed["content"]; exists {
			markContentBearing(document, content)
		}
		if text, exists := typed["text"]; exists {
			markContentBearing(document, text)
		}
		if _, hasContent := typed["content"]; !hasContent {
			if _, hasText := typed["text"]; !hasText && responsesItemRequiresExtraction(typeName, role, typed) {
				markIncompleteContent(document)
			}
		}
		if typeName == "output_text" {
			role = "assistant"
		}
		controlled := true
		source := SourceMessage
		switch role {
		case "system", "developer":
			source = SourceInstruction
			current = true
		}
		appendContent(document, typed["content"], role, source, current, controlled)
		if text, exists := typed["text"]; exists {
			appendStructured(document, text, role, source, current, controlled)
		}
		switch typeName {
		case "":
			markUnknownNonEmptyFields(document, typed, "type", "role", "content", "text")
		case "message":
			markUnknownNonEmptyFields(document, typed, "type", "id", "status", "role", "content", "text")
		case "text", "input_text", "output_text":
			markUnknownNonEmptyFields(document, typed, "type", "text", "annotations", "logprobs")
		default:
			if isImageType(typeName) {
				markUnknownNonEmptyFields(document, typed, "type", "image_url", "url", "file_id", "detail", "source", "data", "media_type", "mime_type", "filename", "text", "content")
			} else if typeName != "" {
				markIncompleteContent(document)
			}
		}
	default:
		if hasNonEmptyValue(typed) {
			markIncompleteContent(document)
		}
	}
}

func isResponsesToolOutput(value any) bool {
	item, ok := value.(map[string]any)
	if !ok {
		return false
	}
	switch normalizedType(item["type"]) {
	case "function_call_output", "custom_tool_call_output", "tool_search_output", "mcp_tool_call_output",
		"local_shell_call_output", "shell_call_output", "apply_patch_call_output", "computer_call_output",
		"program_output", "mcp_approval_response", "mcp_call":
		return true
	default:
		return false
	}
}

func extractLiveRoot(document *Document, root map[string]any) {
	switch normalizedType(root["type"]) {
	case "", "realtime", "transcription":
		extractLiveSession(document, root)
	case "session.update", "transcription_session.update":
		session, ok := root["session"].(map[string]any)
		if !ok {
			markIncompleteContent(document)
			return
		}
		extractLiveSession(document, session)
		markUnknownNonEmptyFields(document, root, "type", "event_id", "session")
	case "conversation.item.create":
		item, ok := root["item"].(map[string]any)
		if !ok {
			markIncompleteContent(document)
			return
		}
		appendResponsesItem(document, item, true)
		markUnknownNonEmptyFields(document, root, "type", "event_id", "item", "previous_item_id")
	case "response.create":
		extractLiveResponseCreate(document, root)
	case "input_audio_buffer.append":
		markUnknownNonEmptyFields(document, root, "type", "event_id", "audio")
	case "input_audio_buffer.commit", "input_audio_buffer.clear", "session.close":
		markUnknownNonEmptyFields(document, root, "type", "event_id")
	case "output_audio_buffer.clear":
		markUnknownNonEmptyFields(document, root, "type", "event_id", "response_id")
	case "conversation.item.retrieve", "conversation.item.delete":
		markUnknownNonEmptyFields(document, root, "type", "event_id", "item_id")
	case "conversation.item.truncate":
		markUnknownNonEmptyFields(document, root, "type", "event_id", "item_id", "content_index", "audio_end_ms")
	case "response.cancel":
		markUnknownNonEmptyFields(document, root, "type", "event_id", "response_id")
	default:
		// New client event types must be explicitly classified before blocking
		// audit modes can forward them to the upstream Live control channel.
		if hasAnyKey(root, "instructions", "tools", "prompt", "input", "input_audio_transcription", "audio") {
			extractLiveSession(document, root)
		}
		markIncompleteContent(document)
	}
}

func extractLiveSession(document *Document, session map[string]any) {
	extractLiveSessionFields(document, session)
	markUnknownLiveSessionFields(document, session, false)
}

func extractLiveResponseCreate(document *Document, root map[string]any) {
	extractLiveSessionFields(document, root)
	markUnknownLiveSessionFields(document, root, true)
	responseValue, exists := root["response"]
	if !exists || !hasNonEmptyValue(responseValue) {
		return
	}
	response, ok := responseValue.(map[string]any)
	if !ok {
		markIncompleteContent(document)
		return
	}
	extractLiveSession(document, response)
}

func extractLiveSessionFields(document *Document, session map[string]any) {
	if instructions, exists := session["instructions"]; exists {
		markContentBearing(document, instructions)
		appendContent(document, instructions, "system", SourceInstruction, true, true)
	}
	appendToolDefinitions(document, session["tools"])
	appendResponsesPrompt(document, session["prompt"])
	if input, exists := session["input"]; exists {
		extractResponsesValue(document, input)
	}
	extractLiveTranscription(document, session["input_audio_transcription"])
	extractLiveAudio(document, session["audio"])
	markKnownStringOrObjectFields(document, session["input_audio_format"], "type", "rate")
	markKnownStringOrObjectFields(document, session["output_audio_format"], "type", "rate")
	markKnownStringOrObjectFields(document, session["voice"], "id")
	markKnownConfigObjectFields(document, session["turn_detection"],
		"type", "create_response", "idle_timeout_ms", "interrupt_response",
		"prefix_padding_ms", "silence_duration_ms", "threshold", "eagerness",
	)
	markKnownConfigObjectFields(document, session["delegation"], "type")
	markKnownConfigObjectFields(document, session["reasoning"], "effort")
	markKnownStringOrObjectFields(document, session["tracing"], "group_id", "metadata", "workflow_name")
	markKnownStringOrObjectFields(document, session["truncation"], "type", "retention_ratio", "token_limits")
}

func markUnknownLiveSessionFields(document *Document, session map[string]any, envelope bool) {
	allowed := []string{
		"type", "model", "instructions", "tools", "tool_choice", "prompt", "input",
		"audio", "input_audio_format", "output_audio_format", "input_audio_transcription",
		"turn_detection", "modalities", "output_modalities", "voice", "speed", "temperature",
		"max_response_output_tokens", "max_output_tokens", "include", "parallel_tool_calls",
		"reasoning", "tracing", "truncation", "delegation", "conversation", "metadata",
	}
	if envelope {
		allowed = append(allowed, "event_id", "response")
	}
	markUnknownNonEmptyFields(document, session,
		allowed...,
	)
}

func extractLiveAudio(document *Document, value any) {
	if !hasNonEmptyValue(value) {
		return
	}
	audio, ok := value.(map[string]any)
	if !ok {
		markIncompleteContent(document)
		return
	}
	markUnknownNonEmptyFields(document, audio, "input", "output")

	if inputValue, exists := audio["input"]; exists && hasNonEmptyValue(inputValue) {
		input, inputOK := inputValue.(map[string]any)
		if !inputOK {
			markIncompleteContent(document)
		} else {
			extractLiveTranscription(document, input["transcription"])
			markKnownStringOrObjectFields(document, input["format"], "type", "rate")
			markKnownConfigObjectFields(document, input["noise_reduction"], "type")
			markKnownConfigObjectFields(document, input["turn_detection"],
				"type", "create_response", "idle_timeout_ms", "interrupt_response",
				"prefix_padding_ms", "silence_duration_ms", "threshold", "eagerness",
			)
			markUnknownNonEmptyFields(document, input, "format", "noise_reduction", "transcription", "turn_detection")
		}
	}
	if outputValue, exists := audio["output"]; exists && hasNonEmptyValue(outputValue) {
		output, outputOK := outputValue.(map[string]any)
		if !outputOK {
			markIncompleteContent(document)
		} else {
			markKnownStringOrObjectFields(document, output["format"], "type", "rate")
			markKnownStringOrObjectFields(document, output["voice"], "id")
			markUnknownNonEmptyFields(document, output, "format", "speed", "voice")
		}
	}
}

func extractLiveTranscription(document *Document, value any) {
	if !hasNonEmptyValue(value) {
		return
	}
	transcription, ok := value.(map[string]any)
	if !ok {
		markIncompleteContent(document)
		return
	}
	for _, key := range []string{"prompt", "keywords"} {
		content, exists := transcription[key]
		if !exists {
			continue
		}
		markContentBearing(document, content)
		appendContent(document, content, "system", SourceInstruction, true, true)
	}
	markUnknownNonEmptyFields(document, transcription,
		"delay", "keywords", "language", "languages", "model", "prompt",
	)
}

func markKnownConfigObjectFields(document *Document, value any, allowed ...string) {
	if !hasNonEmptyValue(value) {
		return
	}
	object, ok := value.(map[string]any)
	if !ok {
		markIncompleteContent(document)
		return
	}
	markUnknownNonEmptyFields(document, object, allowed...)
}

func markKnownStringOrObjectFields(document *Document, value any, allowed ...string) {
	if !hasNonEmptyValue(value) {
		return
	}
	switch typed := value.(type) {
	case string:
		return
	case map[string]any:
		markUnknownNonEmptyFields(document, typed, allowed...)
	default:
		markIncompleteContent(document)
	}
}

func extractAlphaSearch(document *Document, root map[string]any) {
	commandsValue, commandsExist := root["commands"]
	commands, ok := commandsValue.(map[string]any)
	if !ok {
		if commandsExist && hasNonEmptyValue(commandsValue) {
			markIncompleteContent(document)
		}
	} else {
		for key, value := range commands {
			if key != "search_query" && hasNonEmptyValue(value) {
				markIncompleteContent(document)
			}
		}
		queriesValue, queriesExist := commands["search_query"]
		queries, queriesOK := asSlice(queriesValue)
		if !queriesOK {
			if queriesExist && hasNonEmptyValue(queriesValue) {
				markIncompleteContent(document)
			}
		} else {
			for _, value := range queries {
				query, queryOK := value.(map[string]any)
				if !queryOK {
					if hasNonEmptyValue(value) {
						markIncompleteContent(document)
					}
					continue
				}
				q, exists := query["q"]
				if !exists && hasNonEmptyValue(query) {
					markIncompleteContent(document)
					continue
				}
				markContentBearing(document, q)
				appendStructured(document, q, "user", SourceSearchQuery, true, true)
			}
		}
	}
	if input, exists := root["input"]; exists {
		extractResponsesValue(document, input)
	}
}

func extractEmbeddings(document *Document, root map[string]any) {
	input, exists := root["input"]
	if !exists || !hasNonEmptyValue(input) {
		return
	}
	switch typed := input.(type) {
	case []any:
		for _, value := range typed {
			text, ok := value.(string)
			if ok {
				markContentBearing(document, text)
				appendText(document, text, "user", SourceEmbeddingInput, true, true)
				continue
			}
			if hasNonEmptyValue(value) {
				markIncompleteContent(document)
			}
		}
	case string:
		markContentBearing(document, typed)
		appendText(document, typed, "user", SourceEmbeddingInput, true, true)
	default:
		markIncompleteContent(document)
	}
}

func extractGeminiRoot(document *Document, root map[string]any) {
	for _, key := range []string{"systemInstruction", "system_instruction"} {
		if value, exists := root[key]; exists {
			markContentBearing(document, value)
			appendGeminiSystem(document, value)
		}
	}
	appendToolDefinitions(document, root["tools"])
	for _, key := range []string{"contents", "content"} {
		if value, exists := root[key]; exists {
			appendGeminiContents(document, value)
		}
	}
	appendGeminiInstances(document, root["instances"])
	if requestsValue, exists := root["requests"]; exists && hasNonEmptyValue(requestsValue) {
		requests, ok := asSlice(requestsValue)
		if !ok {
			markIncompleteContent(document)
			return
		}
		for _, value := range requests {
			request, ok := value.(map[string]any)
			if !ok {
				markIncompleteContent(document)
				continue
			}
			for _, key := range []string{"systemInstruction", "system_instruction"} {
				if system, exists := request[key]; exists {
					markContentBearing(document, system)
					appendGeminiSystem(document, system)
				}
			}
			appendToolDefinitions(document, request["tools"])
			for _, key := range []string{"contents", "content"} {
				if contents, exists := request[key]; exists {
					appendGeminiContents(document, contents)
				}
			}
			appendGeminiInstances(document, request["instances"])
		}
	}
}

func appendGeminiSystem(document *Document, value any) {
	if object, ok := value.(map[string]any); ok {
		if parts, exists := object["parts"]; exists {
			appendGeminiParts(document, parts, "system", SourceInstruction, true)
			return
		}
	}
	appendContent(document, value, "system", SourceInstruction, true, true)
}

func appendGeminiContents(document *Document, value any) {
	contents, ok := asSlice(value)
	if !ok {
		if object, objectOK := value.(map[string]any); objectOK {
			contents = []any{object}
		} else {
			if hasNonEmptyValue(value) {
				markIncompleteContent(document)
			}
			return
		}
	}
	if len(contents) == 0 {
		return
	}
	for index, value := range contents {
		content, ok := value.(map[string]any)
		if !ok {
			if hasNonEmptyValue(value) {
				markIncompleteContent(document)
			}
			continue
		}
		parts, hasParts := content["parts"]
		if !hasParts {
			if len(content) > 1 || normalizedRole(content["role"]) == "" {
				markIncompleteContent(document)
			}
			continue
		}
		appendGeminiParts(document, parts, normalizedRole(content["role"]), SourceMessage, index == len(contents)-1)
	}
}

func appendGeminiParts(document *Document, value any, role string, source Source, current bool) {
	parts, ok := asSlice(value)
	if !ok {
		if hasNonEmptyValue(value) {
			markIncompleteContent(document)
		}
		return
	}
	for _, value := range parts {
		part, ok := value.(map[string]any)
		if !ok {
			if hasNonEmptyValue(value) {
				markIncompleteContent(document)
			}
			continue
		}
		recognized := false
		hasFunctionCall := false
		hasFunctionResponse := false
		if text, exists := part["text"]; exists {
			recognized = true
			markContentBearing(document, text)
			textRole, textSource := role, source
			if thought, _ := part["thought"].(bool); thought {
				textRole, textSource = "model", SourceReasoning
			}
			appendStructured(document, text, textRole, textSource, current, true)
		}
		for _, key := range []string{"functionCall", "function_call"} {
			if call, ok := part[key].(map[string]any); ok {
				recognized = true
				hasFunctionCall = true
				arguments := firstExisting(call, "args", "arguments")
				if arguments == nil {
					markIncompleteContent(document)
					continue
				}
				markContentBearing(document, arguments)
				appendStructured(document, arguments, roleOr(role, "model"), SourceToolCall, current, true)
			}
		}
		for _, key := range []string{"functionResponse", "function_response"} {
			if response, ok := part[key].(map[string]any); ok {
				recognized = true
				hasFunctionResponse = true
				output, exists := response["response"]
				if !exists {
					markIncompleteContent(document)
					continue
				}
				markContentBearing(document, output)
				appendToolOutput(document, output, current)
			}
		}
		imageRole, imageSource := role, source
		if thought, _ := part["thought"].(bool); thought {
			imageRole, imageSource = "model", SourceReasoning
		}
		switch {
		case hasFunctionResponse:
			imageRole, imageSource = "tool", SourceToolOutput
		case hasFunctionCall:
			imageRole, imageSource = roleOr(role, "model"), SourceToolCall
		}
		appendImageValues(document, part, imageRole, imageSource, current, true, false)
		if !recognized && !isGeminiMediaPart(part) && hasNonEmptyValue(part) {
			markIncompleteContent(document)
		}
		if recognized {
			markUnknownNonEmptyFields(document, part,
				"text", "functionCall", "function_call", "functionResponse", "function_response",
				"inlineData", "inline_data", "fileData", "file_data", "thought", "thoughtSignature",
				"videoMetadata", "video_metadata")
		}
	}
}

func appendGeminiInstances(document *Document, value any) {
	instances, ok := asSlice(value)
	if !ok {
		if hasNonEmptyValue(value) {
			markIncompleteContent(document)
		}
		return
	}
	if len(instances) == 0 {
		return
	}
	for _, value := range instances {
		instance, ok := value.(map[string]any)
		if !ok {
			if hasNonEmptyValue(value) {
				markIncompleteContent(document)
			}
			continue
		}
		prompt, exists := instance["prompt"]
		if !exists && hasNonEmptyValue(instance) {
			markIncompleteContent(document)
			continue
		}
		markContentBearing(document, prompt)
		appendStructured(document, prompt, "user", SourceMessage, true, true)
	}
}

func extractMediaPrompts(document *Document, root map[string]any) {
	for _, key := range []string{"image", "images", "mask", "reference_images"} {
		appendImageValues(document, root[key], "user", SourceMediaPrompt, true, true, true)
	}
	seen := make(map[string]struct{})
	var walk func(any, string)
	walk = func(value any, key string) {
		switch typed := value.(type) {
		case map[string]any:
			keys := make([]string, 0, len(typed))
			for childKey := range typed {
				keys = append(keys, childKey)
			}
			sort.Strings(keys)
			for _, childKey := range keys {
				walk(typed[childKey], childKey)
			}
		case []any:
			for _, item := range typed {
				walk(item, key)
			}
		case string:
			if !isMediaPromptKey(key) || looksLikeMediaPayload(typed) {
				return
			}
			text := strings.TrimSpace(typed)
			if text == "" {
				return
			}
			document.ContentBearing = true
			if _, duplicate := seen[text]; duplicate {
				return
			}
			seen[text] = struct{}{}
			appendText(document, text, "user", SourceMediaPrompt, true, true)
		}
	}
	walk(root, "")
}

func appendToolDefinitions(document *Document, value any) {
	if !hasNonEmptyValue(value) {
		return
	}
	document.ContentBearing = true
	appendStructured(document, value, "system", SourceToolDefinition, true, true)
}

func markContentBearing(document *Document, value any) {
	if document != nil && contentRequiresExtraction(value) {
		document.ContentBearing = true
	}
}

func markIncompleteContent(document *Document) {
	if document == nil {
		return
	}
	document.ContentBearing = true
	document.Incomplete = true
}

func contentRequiresExtraction(value any) bool {
	switch typed := value.(type) {
	case nil:
		return false
	case string:
		return strings.TrimSpace(typed) != ""
	case []any:
		for _, item := range typed {
			if contentRequiresExtraction(item) {
				return true
			}
		}
		return false
	case map[string]any:
		typeName := normalizedType(typed["type"])
		if typeName == "redacted_thinking" || typeName == "compaction" {
			return false
		}
		if typeName == "tool_result" {
			return contentRequiresExtraction(typed["content"])
		}
		if text, exists := typed["text"]; exists && contentRequiresExtraction(text) {
			return true
		}
		if content, exists := typed["content"]; exists && contentRequiresExtraction(content) {
			return true
		}
		if isImageType(typeName) {
			return false
		}
		if typeName == "text" || typeName == "input_text" || typeName == "output_text" || typeName == "message" {
			return false
		}
		return len(typed) > 0
	default:
		return true
	}
}

func responsesItemRequiresExtraction(typeName, role string, item map[string]any) bool {
	if isImageType(typeName) {
		return false
	}
	switch typeName {
	case "message", "input_text", "output_text":
		return false
	case "":
		return role == "" && len(item) > 0
	default:
		return len(item) > 0
	}
}

func isGeminiMediaPart(part map[string]any) bool {
	return hasAnyKey(part, "inlineData", "inline_data", "fileData", "file_data")
}

func appendContent(document *Document, value any, role string, source Source, current, controlled bool) {
	switch typed := value.(type) {
	case string:
		appendText(document, typed, role, source, current, controlled)
	case []any:
		for _, item := range typed {
			appendContent(document, item, role, source, current, controlled)
		}
	case map[string]any:
		before := len(document.Segments)
		typeName := normalizedType(typed["type"])
		contentRole, contentSource := role, source
		switch typeName {
		case "output_text":
			contentRole = "assistant"
		case "summary_text":
			contentRole, contentSource = "assistant", SourceReasoning
		}
		if typeName != "tool_result" {
			appendImageValues(document, typed, contentRole, contentSource, current, controlled, false)
		}
		if isImageType(typeName) && !hasAnyKey(typed, "text", "content") {
			markUnknownNonEmptyFields(document, typed, "type", "image_url", "url", "file_id", "detail", "source", "data", "media_type", "mime_type", "filename")
			return
		}
		if typeName == "tool_result" {
			content, exists := typed["content"]
			if !exists {
				markIncompleteContent(document)
				return
			}
			appendContent(document, content, "tool", SourceToolOutput, current, true)
			if len(document.Segments) == before && contentRequiresExtraction(content) {
				markIncompleteContent(document)
			}
			markUnknownNonEmptyFields(document, typed, "type", "tool_use_id", "content", "is_error", "cache_control")
			return
		}
		if text, exists := typed["text"]; exists {
			appendStructured(document, text, contentRole, contentSource, current, controlled)
		}
		if content, exists := typed["content"]; exists {
			appendContent(document, content, contentRole, contentSource, current, controlled)
		}
		if typeName == "refusal" {
			refusal, exists := typed["refusal"]
			if !exists {
				markIncompleteContent(document)
			} else {
				appendStructured(document, refusal, "assistant", contentSource, current, controlled)
			}
		}
		if len(document.Segments) == before && contentRequiresExtraction(typed) {
			markIncompleteContent(document)
		}
		switch typeName {
		case "", "text", "input_text", "output_text", "summary_text":
			markUnknownNonEmptyFields(document, typed, "type", "text", "content", "annotations", "logprobs")
		case "message":
			markUnknownNonEmptyFields(document, typed, "type", "id", "status", "role", "content", "text")
		case "refusal":
			markUnknownNonEmptyFields(document, typed, "type", "refusal")
		default:
			if !isImageType(typeName) {
				markIncompleteContent(document)
			}
		}
	default:
		if hasNonEmptyValue(typed) {
			markIncompleteContent(document)
		}
	}
}

func appendStructured(document *Document, value any, role string, source Source, current, controlled bool) {
	if value == nil {
		return
	}
	appendImageValues(document, value, role, source, current, controlled, false)
	if text, ok := value.(string); ok {
		if looksLikeEncodedMediaPayload(text) {
			return
		}
		appendText(document, text, role, source, current, controlled)
		return
	}
	sanitized, keep := sanitizeStructuredValue(value, false)
	if !keep {
		return
	}
	raw, err := json.Marshal(sanitized)
	if err != nil {
		markIncompleteContent(document)
		return
	}
	appendText(document, string(raw), role, source, current, controlled)
}

func appendText(document *Document, text, role string, source Source, current, controlled bool) {
	if document == nil || strings.TrimSpace(text) == "" {
		return
	}
	document.Segments = append(document.Segments, Segment{
		Text: text, Role: role, Source: source, Current: current, ClientControlled: controlled,
	})
}

func appendImageValues(document *Document, value any, role string, source Source, current, controlled, mediaContext bool) {
	if document == nil || value == nil {
		return
	}
	switch typed := value.(type) {
	case string:
		candidate := strings.TrimSpace(typed)
		if mediaContext || strings.HasPrefix(strings.ToLower(candidate), "data:image/") {
			appendImage(document, candidate, role, source, current, controlled)
		}
	case []any:
		for _, item := range typed {
			appendImageValues(document, item, role, source, current, controlled, mediaContext)
		}
	case map[string]any:
		appendImageData(document, typed, role, source, current, controlled)
		objectMedia := mediaContext || isImageMediaType(normalizedType(typed["type"]))
		keys := make([]string, 0, len(typed))
		for key := range typed {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			childMedia := objectMedia || isImageValueField(key)
			appendImageValues(document, typed[key], role, source, current, controlled, childMedia)
		}
	}
}

func appendImageData(document *Document, value map[string]any, role string, source Source, current, controlled bool) {
	mimeType := firstString(value, "media_type", "mediaType", "mime_type", "mimeType")
	if !strings.HasPrefix(strings.ToLower(strings.TrimSpace(mimeType)), "image/") {
		return
	}
	data := firstString(value, "data", "base64")
	if strings.TrimSpace(data) == "" {
		return
	}
	appendImage(document, fmt.Sprintf("data:%s;base64,%s", strings.TrimSpace(mimeType), strings.TrimSpace(data)), role, source, current, controlled)
}

func appendImage(document *Document, value, role string, source Source, current, controlled bool) {
	value = strings.TrimSpace(value)
	lower := strings.ToLower(value)
	if value == "" || !strings.HasPrefix(lower, "data:image/") && !strings.HasPrefix(lower, "http://") && !strings.HasPrefix(lower, "https://") {
		return
	}
	document.ContentBearing = true
	document.Images = append(document.Images, Image{
		URL: value, Role: role, Source: source, Current: current, ClientControlled: controlled,
	})
}

func firstString(values map[string]any, keys ...string) string {
	for _, key := range keys {
		if value, ok := values[key].(string); ok && strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func isImageMediaType(typeName string) bool {
	switch typeName {
	case "image", "image_url", "input_image", "output_image", "computer_screenshot":
		return true
	default:
		return false
	}
}

func isImageValueField(key string) bool {
	normalized := strings.NewReplacer("_", "", "-", "").Replace(strings.ToLower(strings.TrimSpace(key)))
	switch normalized {
	case "image", "images", "imageurl", "screenshot", "partialscreenshot", "partialimage", "mask", "referenceimages", "inlinedata", "filedata", "fileuri":
		return true
	default:
		return false
	}
}

func normalizedRole(value any) string {
	role, _ := value.(string)
	return strings.ToLower(strings.TrimSpace(role))
}

func normalizedType(value any) string {
	typeName, _ := value.(string)
	return strings.ToLower(strings.TrimSpace(typeName))
}

func roleOr(role, fallback string) string {
	if strings.TrimSpace(role) != "" {
		return role
	}
	return fallback
}

func asSlice(value any) ([]any, bool) {
	values, ok := value.([]any)
	return values, ok
}

func firstExisting(values map[string]any, keys ...string) any {
	for _, key := range keys {
		if value, exists := values[key]; exists {
			return value
		}
	}
	return nil
}

func hasAnyKey(values map[string]any, keys ...string) bool {
	for _, key := range keys {
		if _, exists := values[key]; exists {
			return true
		}
	}
	return false
}

func hasNonEmptyValue(value any) bool {
	switch typed := value.(type) {
	case nil:
		return false
	case string:
		return strings.TrimSpace(typed) != ""
	case []any:
		return len(typed) > 0
	case map[string]any:
		return len(typed) > 0
	default:
		return true
	}
}

func markUnknownNonEmptyFields(document *Document, values map[string]any, allowed ...string) {
	if len(values) == 0 {
		return
	}
	allowedSet := make(map[string]struct{}, len(allowed))
	for _, key := range allowed {
		allowedSet[key] = struct{}{}
	}
	for key, value := range values {
		if _, ok := allowedSet[key]; ok || !hasNonEmptyValue(value) {
			continue
		}
		markIncompleteContent(document)
	}
}

func sanitizeStructuredValue(value any, mediaContext bool) (any, bool) {
	switch typed := value.(type) {
	case nil:
		return nil, false
	case string:
		if looksLikeEncodedMediaPayload(typed) || mediaContext {
			return nil, false
		}
		return typed, strings.TrimSpace(typed) != ""
	case []any:
		out := make([]any, 0, len(typed))
		for _, item := range typed {
			if sanitized, keep := sanitizeStructuredValue(item, mediaContext); keep {
				out = append(out, sanitized)
			}
		}
		return out, len(out) > 0
	case map[string]any:
		typeName := normalizedType(typed["type"])
		if typeName == "compaction" || typeName == "redacted_thinking" {
			return nil, false
		}
		mediaObject := mediaContext || isImageType(typeName) || isMediaDescriptor(typed)
		out := make(map[string]any, len(typed))
		for key, item := range typed {
			lowerKey := strings.ToLower(strings.TrimSpace(key))
			if isOpaqueStructuredField(lowerKey) {
				continue
			}
			if mediaObject && lowerKey == "type" {
				continue
			}
			childMedia := isStructuredMediaField(lowerKey) && isLikelyMediaValue(item)
			if mediaObject && isDirectMediaPayloadField(lowerKey) {
				childMedia = true
			}
			if lowerKey == "result" && typeName == "image_generation_call" {
				childMedia = true
			}
			if sanitized, keep := sanitizeStructuredValue(item, childMedia); keep {
				out[key] = sanitized
			}
		}
		return out, len(out) > 0
	default:
		return typed, true
	}
}

func isMediaDescriptor(value map[string]any) bool {
	if !hasAnyKey(value, "data", "base64") {
		return false
	}
	for _, key := range []string{"media_type", "mediaType", "mime_type", "mimeType"} {
		if mime, ok := value[key].(string); ok {
			lower := strings.ToLower(strings.TrimSpace(mime))
			if strings.HasPrefix(lower, "image/") || strings.HasPrefix(lower, "video/") || strings.HasPrefix(lower, "audio/") || lower == "application/pdf" {
				return true
			}
		}
	}
	return false
}

func isOpaqueStructuredField(key string) bool {
	switch key {
	case "encrypted_content", "fingerprint":
		return true
	default:
		return false
	}
}

func isStructuredMediaField(key string) bool {
	switch key {
	case "image_url", "imageurl", "file_url", "fileurl", "file_id", "fileid", "file_data", "filedata", "base64":
		return true
	default:
		return false
	}
}

func isDirectMediaPayloadField(key string) bool {
	switch key {
	case "image_url", "imageurl", "url", "file_url", "fileurl", "file_id", "fileid",
		"file_data", "filedata", "data", "base64", "source", "media_type", "mediatype",
		"mime_type", "mimetype", "filename":
		return true
	default:
		return false
	}
}

func isLikelyMediaValue(value any) bool {
	switch typed := value.(type) {
	case string:
		return true
	case map[string]any:
		return isImageType(normalizedType(typed["type"])) || hasAnyKey(typed, "url", "image_url", "file_id", "file_data", "data", "base64")
	case []any:
		for _, item := range typed {
			if isLikelyMediaValue(item) {
				return true
			}
		}
	}
	return false
}

func isImageType(typeName string) bool {
	switch typeName {
	case "image", "image_url", "input_image", "output_image", "video", "input_video",
		"input_file", "file", "computer_screenshot", "input_audio", "audio":
		return true
	default:
		return false
	}
}

func isMediaPromptKey(key string) bool {
	normalized := strings.NewReplacer("_", "", "-", "").Replace(strings.ToLower(strings.TrimSpace(key)))
	switch normalized {
	case "prompt", "inputprompt", "textprompt", "description", "query", "lyrics", "negativeprompt",
		"positiveprompt", "gptdescriptionprompt", "prompten", "finalprompt", "finalzhprompt",
		"origprompt", "actualprompt", "imageprompt", "input":
		return true
	default:
		return false
	}
}

func looksLikeMediaPayload(value string) bool {
	trimmed := strings.TrimSpace(value)
	lower := strings.ToLower(trimmed)
	if strings.HasPrefix(lower, "data:image/") || strings.HasPrefix(lower, "data:video/") ||
		strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://") {
		return true
	}
	if len(trimmed) < 256 {
		return false
	}
	for _, r := range trimmed {
		alphaNumeric := r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z' || r >= '0' && r <= '9'
		if !alphaNumeric && r != '+' && r != '/' && r != '=' {
			return false
		}
	}
	return true
}

func looksLikeEncodedMediaPayload(value string) bool {
	trimmed := strings.TrimSpace(value)
	lower := strings.ToLower(trimmed)
	if strings.HasPrefix(lower, "data:image/") || strings.HasPrefix(lower, "data:video/") ||
		strings.HasPrefix(lower, "data:audio/") || strings.HasPrefix(lower, "data:application/pdf") {
		return true
	}
	if len(trimmed) < 256 {
		return false
	}
	for _, r := range trimmed {
		alphaNumeric := r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z' || r >= '0' && r <= '9'
		if !alphaNumeric && r != '+' && r != '/' && r != '=' {
			return false
		}
	}
	return true
}
