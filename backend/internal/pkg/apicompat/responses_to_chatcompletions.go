package apicompat

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// ---------------------------------------------------------------------------
// Non-streaming: ResponsesResponse → ChatCompletionsResponse
// ---------------------------------------------------------------------------

// ResponsesToChatCompletions converts a Responses API response into a Chat
// Completions response. Text output items are concatenated into
// choices[0].message.content; function_call items become tool_calls.
func ResponsesToChatCompletions(resp *ResponsesResponse, model string) *ChatCompletionsResponse {
	id := resp.ID
	if id == "" {
		id = generateChatCmplID()
	}

	out := &ChatCompletionsResponse{
		ID:          id,
		Object:      "chat.completion",
		Created:     time.Now().Unix(),
		Model:       model,
		ServiceTier: resp.ServiceTier,
	}

	var contentText string
	var reasoningText string
	var toolCalls []ChatToolCall

	for _, item := range resp.Output {
		switch item.Type {
		case "message":
			for _, part := range item.Content {
				switch part.Type {
				case "output_text":
					contentText += part.Text
				case "refusal":
					contentText += part.Refusal
				}
			}
		case "function_call", "custom_tool_call", "tool_search_call":
			toolCalls = append(toolCalls, ChatToolCall{
				ID:   item.CallID,
				Type: "function",
				Function: ChatFunctionCall{
					Name:      item.Name,
					Arguments: responsesToolCallArguments(&item),
				},
			})
		case "reasoning":
			for _, s := range item.Summary {
				if s.Type == "summary_text" && s.Text != "" {
					reasoningText += s.Text
				}
			}
		case "web_search_call":
			// silently consumed — results already incorporated into text output
		}
	}

	msg := ChatMessage{Role: "assistant"}
	if len(toolCalls) > 0 {
		msg.ToolCalls = toolCalls
	}
	if contentText != "" {
		raw, _ := json.Marshal(contentText)
		msg.Content = raw
	}
	if reasoningText != "" {
		msg.ReasoningContent = reasoningText
	}

	finishReason := responsesStatusToChatFinishReason(resp.Status, resp.IncompleteDetails, toolCalls)

	out.Choices = []ChatChoice{{
		Index:        0,
		Message:      msg,
		FinishReason: finishReason,
	}}

	out.Usage = chatUsageFromResponsesUsage(resp.Usage)

	return out
}

func responsesStatusToChatFinishReason(status string, details *ResponsesIncompleteDetails, toolCalls []ChatToolCall) string {
	switch status {
	case "incomplete":
		if details != nil {
			switch details.Reason {
			case "max_output_tokens":
				return "length"
			case "content_filter":
				return "content_filter"
			}
		}
		return "stop"
	case "completed":
		if len(toolCalls) > 0 {
			return "tool_calls"
		}
		return "stop"
	default:
		return "stop"
	}
}

// ---------------------------------------------------------------------------
// Streaming: ResponsesStreamEvent → []ChatCompletionsChunk (stateful converter)
// ---------------------------------------------------------------------------

// ResponsesEventToChatState tracks state for converting a sequence of Responses
// SSE events into Chat Completions SSE chunks.
type ResponsesEventToChatState struct {
	ID                     string
	Model                  string
	Created                int64
	ServiceTier            string // upstream tier observed on response events; echoed on chunks
	SentRole               bool
	SawToolCall            bool
	SawText                bool
	SawReasoning           bool
	Finalized              bool        // true after finish chunk has been emitted
	NextToolCallIndex      int         // next sequential tool_call index to assign
	OutputIndexToToolIndex map[int]int // Responses output_index → Chat tool_calls index
	ToolNamesSeen          map[int]bool
	ToolArguments          map[int]string
	TextParts              map[responsesOutputPartKey]string
	ReasoningParts         map[responsesOutputPartKey]string
	TextOutput             strings.Builder
	ReasoningOutput        strings.Builder
	IncludeUsage           bool
	Usage                  *ChatUsage
}

type responsesOutputPartKey struct {
	OutputIndex int
	PartIndex   int
	PartType    string
}

// NewResponsesEventToChatState returns an initialised stream state.
func NewResponsesEventToChatState() *ResponsesEventToChatState {
	return &ResponsesEventToChatState{
		ID:                     generateChatCmplID(),
		Created:                time.Now().Unix(),
		OutputIndexToToolIndex: make(map[int]int),
		ToolNamesSeen:          make(map[int]bool),
		ToolArguments:          make(map[int]string),
		TextParts:              make(map[responsesOutputPartKey]string),
		ReasoningParts:         make(map[responsesOutputPartKey]string),
	}
}

// ResponsesEventToChatChunks converts a single Responses SSE event into zero
// or more Chat Completions chunks, updating state as it goes.
func ResponsesEventToChatChunks(evt *ResponsesStreamEvent, state *ResponsesEventToChatState) []ChatCompletionsChunk {
	switch evt.Type {
	case "response.created":
		return resToChatHandleCreated(evt, state)
	case "response.output_text.delta", "response.refusal.delta":
		return resToChatHandleTextDelta(evt, state)
	case "response.output_text.done", "response.refusal.done":
		return resToChatHandleTextDone(evt, state)
	case "response.output_item.added":
		return resToChatHandleOutputItemAdded(evt, state)
	case "response.output_item.done":
		return resToChatHandleOutputItemDone(evt, state)
	case "response.content_part.done":
		return resToChatHandleContentPartAggregate(evt, state)
	case "response.function_call_arguments.delta",
		// custom/freeform 工具（如新版 apply_patch）的输入增量与 function_call 参数增量同形，
		// 均按 OutputIndex 累加到对应工具调用。
		"response.custom_tool_call_input.delta", "response.tool_search_call_arguments.delta":
		return resToChatHandleFuncArgsDelta(evt, state)
	case "response.function_call_arguments.done", "response.custom_tool_call_input.done", "response.tool_search_call_arguments.done":
		return resToChatHandleFuncArgsDone(evt, state)
	case "response.reasoning_summary_text.delta",
		// 原始推理文本增量（真实 Codex 客户端消费的 reasoning_text.delta），
		// 与 reasoning summary 一样映射为 reasoning_content。
		"response.reasoning_text.delta":
		return resToChatHandleReasoningDelta(evt, state)
	case "response.reasoning_summary_text.done", "response.reasoning_text.done":
		return resToChatHandleReasoningDone(evt, state)
	// response.done 是 Realtime/WS 与项目透传路径使用的终止别名；
	// 普通 Responses HTTP SSE 的公开终止事件仍以 response.completed 为主。
	case "response.completed", "response.done", "response.incomplete", "response.failed":
		return resToChatHandleCompleted(evt, state)
	default:
		return nil
	}
}

// FinalizeResponsesChatStream emits a final chunk with finish_reason if the
// stream ended without a proper completion event (e.g. upstream disconnect).
// It is idempotent: if a completion event already emitted the finish chunk,
// this returns nil.
func FinalizeResponsesChatStream(state *ResponsesEventToChatState) []ChatCompletionsChunk {
	if state.Finalized {
		return nil
	}
	state.Finalized = true

	finishReason := "stop"
	if state.SawToolCall {
		finishReason = "tool_calls"
	}

	chunks := []ChatCompletionsChunk{makeChatFinishChunk(state, finishReason)}

	if state.IncludeUsage && state.Usage != nil {
		chunks = append(chunks, ChatCompletionsChunk{
			ID:          state.ID,
			Object:      "chat.completion.chunk",
			Created:     state.Created,
			Model:       state.Model,
			ServiceTier: state.ServiceTier,
			Choices:     []ChatChunkChoice{},
			Usage:       state.Usage,
		})
	}

	return chunks
}

// ChatChunkToSSE formats a ChatCompletionsChunk as an SSE data line.
func ChatChunkToSSE(chunk ChatCompletionsChunk) (string, error) {
	data, err := json.Marshal(chunk)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("data: %s\n\n", data), nil
}

// --- internal handlers ---

func resToChatHandleCreated(evt *ResponsesStreamEvent, state *ResponsesEventToChatState) []ChatCompletionsChunk {
	if evt.Response != nil {
		if evt.Response.ID != "" {
			state.ID = evt.Response.ID
		}
		if state.Model == "" && evt.Response.Model != "" {
			state.Model = evt.Response.Model
		}
		if evt.Response.ServiceTier != "" {
			state.ServiceTier = evt.Response.ServiceTier
		}
	}
	// Emit the role chunk.
	if state.SentRole {
		return nil
	}
	state.SentRole = true

	role := "assistant"
	return []ChatCompletionsChunk{makeChatDeltaChunk(state, ChatDelta{Role: role})}
}

func resToChatHandleTextDelta(evt *ResponsesStreamEvent, state *ResponsesEventToChatState) []ChatCompletionsChunk {
	if evt.Delta == "" {
		return nil
	}
	state.SawText = true
	key := responsesOutputPartKey{OutputIndex: evt.OutputIndex, PartIndex: evt.ContentIndex, PartType: responsesTextPartType(evt.Type)}
	state.TextParts[key] += evt.Delta
	_, _ = state.TextOutput.WriteString(evt.Delta)
	content := evt.Delta
	return []ChatCompletionsChunk{makeChatDeltaChunk(state, ChatDelta{Content: &content})}
}

func resToChatHandleTextDone(evt *ResponsesStreamEvent, state *ResponsesEventToChatState) []ChatCompletionsChunk {
	text := evt.Text
	if text == "" {
		text = evt.Refusal
	}
	if text == "" {
		return nil
	}
	key := responsesOutputPartKey{OutputIndex: evt.OutputIndex, PartIndex: evt.ContentIndex, PartType: responsesTextPartType(evt.Type)}
	suffix, ok := aggregateOutputSuffix(state.TextParts[key], text)
	if !ok || suffix == "" {
		return nil
	}
	state.TextParts[key] = text
	_, _ = state.TextOutput.WriteString(suffix)
	state.SawText = true
	return []ChatCompletionsChunk{makeChatAggregateDeltaChunk(state, ChatDelta{Content: &suffix})}
}

func resToChatHandleOutputItemAdded(evt *ResponsesStreamEvent, state *ResponsesEventToChatState) []ChatCompletionsChunk {
	// function_call 与 custom_tool_call（custom/freeform 工具）均按工具调用注册，
	// 以便后续 *_input.delta / *_arguments.delta 能映射到正确的工具索引。
	if evt.Item == nil || !isResponsesToolCall(evt.Item.Type) {
		return nil
	}

	state.SawToolCall = true
	idx := state.NextToolCallIndex
	state.OutputIndexToToolIndex[evt.OutputIndex] = idx
	state.NextToolCallIndex++
	state.ToolNamesSeen[evt.OutputIndex] = evt.Item.Name != ""

	return []ChatCompletionsChunk{makeChatDeltaChunk(state, ChatDelta{
		ToolCalls: []ChatToolCall{{
			Index: &idx,
			ID:    evt.Item.CallID,
			Type:  "function",
			Function: ChatFunctionCall{
				Name: evt.Item.Name,
			},
		}},
	})}
}

func resToChatHandleOutputItemDone(evt *ResponsesStreamEvent, state *ResponsesEventToChatState) []ChatCompletionsChunk {
	if evt.Item == nil {
		return nil
	}
	return resToChatSupplementOutputItem(evt.Item, evt.OutputIndex, state)
}

func resToChatSupplementOutputItem(item *ResponsesOutput, outputIndex int, state *ResponsesEventToChatState) []ChatCompletionsChunk {
	if item == nil {
		return nil
	}

	switch item.Type {
	case "message":
		var text strings.Builder
		for partIndex, part := range item.Content {
			var value string
			switch part.Type {
			case "output_text":
				value = part.Text
			case "refusal":
				value = part.Refusal
			default:
				continue
			}
			key := responsesOutputPartKey{OutputIndex: outputIndex, PartIndex: partIndex, PartType: part.Type}
			suffix, compatible := aggregateOutputSuffix(state.TextParts[key], value)
			if !compatible {
				continue
			}
			state.TextParts[key] = value
			_, _ = text.WriteString(suffix)
		}
		if text.Len() == 0 {
			return nil
		}
		state.SawText = true
		value := text.String()
		_, _ = state.TextOutput.WriteString(value)
		return []ChatCompletionsChunk{makeChatAggregateDeltaChunk(state, ChatDelta{Content: &value})}
	case "reasoning":
		var reasoning strings.Builder
		for summaryIndex, summary := range item.Summary {
			if summary.Type != "summary_text" {
				continue
			}
			key := responsesOutputPartKey{OutputIndex: outputIndex, PartIndex: summaryIndex, PartType: "reasoning_summary"}
			suffix, compatible := aggregateOutputSuffix(state.ReasoningParts[key], summary.Text)
			if !compatible {
				continue
			}
			state.ReasoningParts[key] = summary.Text
			_, _ = reasoning.WriteString(suffix)
		}
		if reasoning.Len() == 0 {
			return nil
		}
		state.SawReasoning = true
		value := reasoning.String()
		_, _ = state.ReasoningOutput.WriteString(value)
		return []ChatCompletionsChunk{makeChatAggregateDeltaChunk(state, ChatDelta{ReasoningContent: &value})}
	case "function_call", "custom_tool_call", "tool_search_call":
		return resToChatSupplementToolCall(outputIndex, item, state)
	default:
		return nil
	}
}

func resToChatHandleContentPartAggregate(evt *ResponsesStreamEvent, state *ResponsesEventToChatState) []ChatCompletionsChunk {
	if evt.Part == nil {
		return nil
	}
	var text string
	switch evt.Part.Type {
	case "output_text":
		text = evt.Part.Text
	case "refusal":
		text = evt.Part.Refusal
	}
	if text == "" {
		return nil
	}
	key := responsesOutputPartKey{OutputIndex: evt.OutputIndex, PartIndex: evt.ContentIndex, PartType: evt.Part.Type}
	suffix, ok := aggregateOutputSuffix(state.TextParts[key], text)
	if !ok || suffix == "" {
		return nil
	}
	state.TextParts[key] = text
	_, _ = state.TextOutput.WriteString(suffix)
	state.SawText = true
	return []ChatCompletionsChunk{makeChatAggregateDeltaChunk(state, ChatDelta{Content: &suffix})}
}

func resToChatHandleFuncArgsDelta(evt *ResponsesStreamEvent, state *ResponsesEventToChatState) []ChatCompletionsChunk {
	if evt.Delta == "" {
		return nil
	}

	idx, ok := state.OutputIndexToToolIndex[evt.OutputIndex]
	if !ok {
		return nil
	}
	state.ToolArguments[evt.OutputIndex] += evt.Delta

	return []ChatCompletionsChunk{makeChatDeltaChunk(state, ChatDelta{
		ToolCalls: []ChatToolCall{{
			Index: &idx,
			Function: ChatFunctionCall{
				Arguments: evt.Delta,
			},
		}},
	})}
}

func resToChatHandleFuncArgsDone(evt *ResponsesStreamEvent, state *ResponsesEventToChatState) []ChatCompletionsChunk {
	arguments := evt.Arguments
	if arguments == "" {
		arguments = evt.Input
	}
	idx, ok := state.OutputIndexToToolIndex[evt.OutputIndex]
	if !ok {
		if evt.Name == "" && evt.CallID == "" {
			return nil
		}
		idx = state.NextToolCallIndex
		state.NextToolCallIndex++
		state.OutputIndexToToolIndex[evt.OutputIndex] = idx
		state.SawToolCall = true
		state.ToolNamesSeen[evt.OutputIndex] = evt.Name != ""
		delta := ChatDelta{ToolCalls: []ChatToolCall{{
			Index: &idx,
			ID:    evt.CallID,
			Type:  "function",
			Function: ChatFunctionCall{
				Name:      evt.Name,
				Arguments: arguments,
			},
		}}}
		state.ToolArguments[evt.OutputIndex] = arguments
		return []ChatCompletionsChunk{makeChatAggregateDeltaChunk(state, delta)}
	}
	suffix, compatible := aggregateOutputSuffix(state.ToolArguments[evt.OutputIndex], arguments)
	name := ""
	if !state.ToolNamesSeen[evt.OutputIndex] && evt.Name != "" {
		name = evt.Name
		state.ToolNamesSeen[evt.OutputIndex] = true
	}
	if (!compatible || suffix == "") && name == "" {
		return nil
	}
	if compatible {
		state.ToolArguments[evt.OutputIndex] = arguments
	}
	return []ChatCompletionsChunk{makeChatAggregateDeltaChunk(state, ChatDelta{
		ToolCalls: []ChatToolCall{{
			Index: &idx,
			Function: ChatFunctionCall{
				Name:      name,
				Arguments: suffix,
			},
		}},
	})}
}

func resToChatHandleReasoningDelta(evt *ResponsesStreamEvent, state *ResponsesEventToChatState) []ChatCompletionsChunk {
	if evt.Delta == "" {
		return nil
	}
	state.SawReasoning = true
	key := responsesOutputPartKey{OutputIndex: evt.OutputIndex, PartIndex: evt.SummaryIndex, PartType: responsesReasoningPartType(evt.Type)}
	state.ReasoningParts[key] += evt.Delta
	_, _ = state.ReasoningOutput.WriteString(evt.Delta)
	reasoning := evt.Delta
	return []ChatCompletionsChunk{makeChatDeltaChunk(state, ChatDelta{ReasoningContent: &reasoning})}
}

func resToChatHandleReasoningDone(evt *ResponsesStreamEvent, state *ResponsesEventToChatState) []ChatCompletionsChunk {
	if evt.Text == "" {
		return nil
	}
	key := responsesOutputPartKey{OutputIndex: evt.OutputIndex, PartIndex: evt.SummaryIndex, PartType: responsesReasoningPartType(evt.Type)}
	suffix, ok := aggregateOutputSuffix(state.ReasoningParts[key], evt.Text)
	if !ok || suffix == "" {
		return nil
	}
	state.ReasoningParts[key] = evt.Text
	_, _ = state.ReasoningOutput.WriteString(suffix)
	state.SawReasoning = true
	reasoning := suffix
	return []ChatCompletionsChunk{makeChatAggregateDeltaChunk(state, ChatDelta{ReasoningContent: &reasoning})}
}

func resToChatHandleCompleted(evt *ResponsesStreamEvent, state *ResponsesEventToChatState) []ChatCompletionsChunk {
	finishReason := "stop"
	var chunks []ChatCompletionsChunk

	if evt.Usage != nil {
		state.Usage = chatUsageFromResponsesUsage(evt.Usage)
	}
	if evt.Response != nil {
		chunks = append(chunks, resToChatSupplementOutputItems(evt.Response.Output, 0, state)...)
		if evt.Response.Usage != nil {
			state.Usage = chatUsageFromResponsesUsage(evt.Response.Usage)
		}
		if evt.Response.ServiceTier != "" {
			state.ServiceTier = evt.Response.ServiceTier
		}

		switch evt.Response.Status {
		case "incomplete":
			if evt.Response.IncompleteDetails != nil {
				switch evt.Response.IncompleteDetails.Reason {
				case "max_output_tokens":
					finishReason = "length"
				case "content_filter":
					finishReason = "content_filter"
				}
			}
		case "completed":
			if state.SawToolCall {
				finishReason = "tool_calls"
			}
		}
	} else if state.SawToolCall {
		finishReason = "tool_calls"
	}
	if state.SawToolCall && finishReason == "stop" && (evt.Response == nil || evt.Response.Status == "completed") {
		finishReason = "tool_calls"
	}

	state.Finalized = true
	chunks = append(chunks, makeChatFinishChunk(state, finishReason))

	if state.IncludeUsage && state.Usage != nil {
		chunks = append(chunks, ChatCompletionsChunk{
			ID:          state.ID,
			Object:      "chat.completion.chunk",
			Created:     state.Created,
			Model:       state.Model,
			ServiceTier: state.ServiceTier,
			Choices:     []ChatChunkChoice{},
			Usage:       state.Usage,
		})
	}

	return chunks
}

func resToChatSupplementOutputItems(items []ResponsesOutput, firstOutputIndex int, state *ResponsesEventToChatState) []ChatCompletionsChunk {
	var chunks []ChatCompletionsChunk
	var text strings.Builder
	var reasoning strings.Builder

	for i := range items {
		item := &items[i]
		switch item.Type {
		case "message":
			for _, part := range item.Content {
				switch part.Type {
				case "output_text":
					_, _ = text.WriteString(part.Text)
				case "refusal":
					_, _ = text.WriteString(part.Refusal)
				}
			}
		case "reasoning":
			for _, summary := range item.Summary {
				if summary.Type == "summary_text" {
					_, _ = reasoning.WriteString(summary.Text)
				}
			}
		case "function_call", "custom_tool_call", "tool_search_call":
			chunks = append(chunks, resToChatSupplementToolCall(firstOutputIndex+i, item, state)...)
		}
	}

	reasoningSuffix, reasoningCompatible := aggregateOutputSuffix(state.ReasoningOutput.String(), reasoning.String())
	if reasoningCompatible && reasoningSuffix != "" {
		state.SawReasoning = true
		_, _ = state.ReasoningOutput.WriteString(reasoningSuffix)
		value := reasoningSuffix
		chunks = append([]ChatCompletionsChunk{makeChatAggregateDeltaChunk(state, ChatDelta{ReasoningContent: &value})}, chunks...)
	}
	textSuffix, textCompatible := aggregateOutputSuffix(state.TextOutput.String(), text.String())
	if textCompatible && textSuffix != "" {
		state.SawText = true
		_, _ = state.TextOutput.WriteString(textSuffix)
		value := textSuffix
		insertAt := 0
		if reasoningCompatible && reasoningSuffix != "" {
			insertAt = 1
		}
		chunks = append(chunks, ChatCompletionsChunk{})
		copy(chunks[insertAt+1:], chunks[insertAt:])
		chunks[insertAt] = makeChatAggregateDeltaChunk(state, ChatDelta{Content: &value})
	}
	return chunks
}

func resToChatSupplementToolCall(outputIndex int, item *ResponsesOutput, state *ResponsesEventToChatState) []ChatCompletionsChunk {
	arguments := responsesToolCallArguments(item)
	idx, exists := state.OutputIndexToToolIndex[outputIndex]
	if exists {
		suffix, compatible := aggregateOutputSuffix(state.ToolArguments[outputIndex], arguments)
		name := ""
		if !state.ToolNamesSeen[outputIndex] && item.Name != "" {
			name = item.Name
			state.ToolNamesSeen[outputIndex] = true
		}
		if (!compatible || suffix == "") && name == "" {
			return nil
		}
		if compatible {
			state.ToolArguments[outputIndex] = arguments
		}
		return []ChatCompletionsChunk{makeChatAggregateDeltaChunk(state, ChatDelta{
			ToolCalls: []ChatToolCall{{
				Index: &idx,
				Function: ChatFunctionCall{
					Name:      name,
					Arguments: suffix,
				},
			}},
		})}
	}

	idx = state.NextToolCallIndex
	state.NextToolCallIndex++
	state.OutputIndexToToolIndex[outputIndex] = idx
	state.SawToolCall = true
	state.ToolNamesSeen[outputIndex] = item.Name != ""
	state.ToolArguments[outputIndex] = arguments
	return []ChatCompletionsChunk{makeChatAggregateDeltaChunk(state, ChatDelta{
		ToolCalls: []ChatToolCall{{
			Index: &idx,
			ID:    item.CallID,
			Type:  "function",
			Function: ChatFunctionCall{
				Name:      item.Name,
				Arguments: arguments,
			},
		}},
	})}
}

func aggregateOutputSuffix(emitted, aggregate string) (string, bool) {
	if aggregate == "" {
		return "", true
	}
	if !strings.HasPrefix(aggregate, emitted) {
		return "", false
	}
	return strings.TrimPrefix(aggregate, emitted), true
}

func responsesTextPartType(eventType string) string {
	if strings.Contains(eventType, "refusal") {
		return "refusal"
	}
	return "output_text"
}

func responsesReasoningPartType(eventType string) string {
	if strings.Contains(eventType, "reasoning_summary") {
		return "reasoning_summary"
	}
	return "reasoning_text"
}

func isResponsesToolCall(kind string) bool {
	return kind == "function_call" || kind == "custom_tool_call" || kind == "tool_search_call"
}

func responsesToolCallArguments(item *ResponsesOutput) string {
	if item == nil {
		return ""
	}
	if item.Type == "custom_tool_call" {
		return item.Input
	}
	return item.Arguments
}

func chatUsageFromResponsesUsage(u *ResponsesUsage) *ChatUsage {
	if u == nil {
		return nil
	}
	usage := &ChatUsage{
		PromptTokens:     u.InputTokens,
		CompletionTokens: u.OutputTokens,
		TotalTokens:      u.InputTokens + u.OutputTokens,
	}
	usage.PromptTokensDetails = promptDetailsFromResponses(u.InputTokensDetails)
	if u.CacheCreationInputTokens > 0 {
		if usage.PromptTokensDetails == nil {
			usage.PromptTokensDetails = &ChatTokenDetails{}
		}
		if usage.PromptTokensDetails.CacheWriteTokens == 0 && usage.PromptTokensDetails.CacheCreationTokens == 0 {
			usage.PromptTokensDetails.CacheCreationTokens = u.CacheCreationInputTokens
		}
	}
	usage.CompletionTokensDetails = completionDetailsFromResponses(u.OutputTokensDetails)
	return usage
}

// promptDetailsFromResponses maps Responses-API input_tokens_details into a
// Chat-Completions prompt_tokens_details. Returns nil when nothing would be
// emitted, so upstreams that do not break down prompt usage stay clean.
func promptDetailsFromResponses(src *ResponsesInputTokensDetails) *ChatTokenDetails {
	if src == nil {
		return nil
	}
	if src.CachedTokens == 0 && src.AudioTokens == 0 && src.CacheCreationTokens == 0 && src.CacheWriteTokens == 0 {
		return nil
	}
	return &ChatTokenDetails{
		CachedTokens:        src.CachedTokens,
		AudioTokens:         src.AudioTokens,
		CacheCreationTokens: src.CacheCreationTokens,
		CacheWriteTokens:    src.CacheWriteTokens,
	}
}

// completionDetailsFromResponses maps Responses-API output_tokens_details
// into a Chat-Completions completion_tokens_details. Mirrors the OpenAI
// official CompletionUsage schema: reasoning_tokens, audio_tokens, and
// the predicted-outputs accepted/rejected counts. Returns nil when nothing
// would be emitted so non-reasoning, non-audio responses stay clean.
func completionDetailsFromResponses(src *ResponsesOutputTokensDetails) *ChatTokenDetails {
	if src == nil {
		return nil
	}
	if src.ReasoningTokens == 0 && src.AudioTokens == 0 &&
		src.AcceptedPredictionTokens == 0 && src.RejectedPredictionTokens == 0 {
		return nil
	}
	return &ChatTokenDetails{
		ReasoningTokens:          src.ReasoningTokens,
		AudioTokens:              src.AudioTokens,
		AcceptedPredictionTokens: src.AcceptedPredictionTokens,
		RejectedPredictionTokens: src.RejectedPredictionTokens,
	}
}

func makeChatDeltaChunk(state *ResponsesEventToChatState, delta ChatDelta) ChatCompletionsChunk {
	return ChatCompletionsChunk{
		ID:          state.ID,
		Object:      "chat.completion.chunk",
		Created:     state.Created,
		Model:       state.Model,
		ServiceTier: state.ServiceTier,
		Choices: []ChatChunkChoice{{
			Index:        0,
			Delta:        delta,
			FinishReason: nil,
		}},
	}
}

func makeChatAggregateDeltaChunk(state *ResponsesEventToChatState, delta ChatDelta) ChatCompletionsChunk {
	chunk := makeChatDeltaChunk(state, delta)
	chunk.AggregateOutput = true
	return chunk
}

func makeChatFinishChunk(state *ResponsesEventToChatState, finishReason string) ChatCompletionsChunk {
	empty := ""
	return ChatCompletionsChunk{
		ID:          state.ID,
		Object:      "chat.completion.chunk",
		Created:     state.Created,
		Model:       state.Model,
		ServiceTier: state.ServiceTier,
		Choices: []ChatChunkChoice{{
			Index:        0,
			Delta:        ChatDelta{Content: &empty},
			FinishReason: &finishReason,
		}},
	}
}

// generateChatCmplID returns a "chatcmpl-" prefixed random hex ID.
func generateChatCmplID() string {
	b := make([]byte, 12)
	_, _ = rand.Read(b)
	return "chatcmpl-" + hex.EncodeToString(b)
}

// ---------------------------------------------------------------------------
// BufferedResponseAccumulator: accumulates SSE delta events for non-streaming
// paths where the terminal event may have empty output.
// ---------------------------------------------------------------------------

type bufferedFuncCall struct {
	Type   string
	CallID string
	Name   string
	Args   strings.Builder
}

// BufferedResponseAccumulator collects content from Responses SSE delta events
// so that non-streaming handlers can reconstruct output when the terminal event
// (response.completed / response.done) carries an empty output array.
type BufferedResponseAccumulator struct {
	text                 strings.Builder
	reasoning            strings.Builder
	funcCalls            []bufferedFuncCall
	outputIndexToFuncIdx map[int]int
	textParts            map[responsesOutputPartKey]string
	reasoningParts       map[responsesOutputPartKey]string
}

// NewBufferedResponseAccumulator returns an initialised accumulator.
func NewBufferedResponseAccumulator() *BufferedResponseAccumulator {
	return &BufferedResponseAccumulator{
		outputIndexToFuncIdx: make(map[int]int),
		textParts:            make(map[responsesOutputPartKey]string),
		reasoningParts:       make(map[responsesOutputPartKey]string),
	}
}

// ProcessEvent inspects a single Responses SSE event and accumulates any
// content it carries. Only delta events that contribute to the final output
// are handled; all other event types are silently ignored.
func (a *BufferedResponseAccumulator) ProcessEvent(event *ResponsesStreamEvent) {
	if a == nil || event == nil {
		return
	}
	switch event.Type {
	case "response.output_text.delta", "response.refusal.delta":
		if event.Delta != "" {
			key := responsesOutputPartKey{OutputIndex: event.OutputIndex, PartIndex: event.ContentIndex, PartType: responsesTextPartType(event.Type)}
			a.appendTextDelta(key, event.Delta)
		}
	case "response.output_text.done", "response.refusal.done":
		value := event.Text
		if value == "" {
			value = event.Refusal
		}
		key := responsesOutputPartKey{OutputIndex: event.OutputIndex, PartIndex: event.ContentIndex, PartType: responsesTextPartType(event.Type)}
		a.appendTextAggregate(key, value)
	case "response.content_part.added", "response.content_part.done":
		a.processContentPart(event)
	case "response.reasoning_summary_part.added", "response.reasoning_summary_part.done":
		a.processReasoningPart(event)
	case "response.output_item.added":
		a.processOutputItem(event)
	case "response.output_item.done":
		a.processOutputItem(event)
	case "response.function_call_arguments.delta", "response.custom_tool_call_input.delta", "response.tool_search_call_arguments.delta":
		if event.Delta != "" {
			if idx, ok := a.outputIndexToFuncIdx[event.OutputIndex]; ok && idx >= 0 && idx < len(a.funcCalls) {
				_, _ = a.funcCalls[idx].Args.WriteString(event.Delta)
			}
		}
	case "response.function_call_arguments.done", "response.custom_tool_call_input.done", "response.tool_search_call_arguments.done":
		arguments := event.Arguments
		if arguments == "" {
			arguments = event.Input
		}
		kind := responsesToolCallTypeForEvent(event.Type)
		idx, ok := a.outputIndexToFuncIdx[event.OutputIndex]
		if !ok && (event.Name != "" || event.CallID != "") {
			idx = a.ensureFuncCall(event.OutputIndex, kind, event.CallID, event.Name)
			ok = idx >= 0
		}
		if ok && idx >= 0 && idx < len(a.funcCalls) {
			a.mergeFuncCallMetadata(idx, kind, event.CallID, event.Name)
			a.appendFuncCallAggregate(idx, arguments)
		}
	case "response.reasoning_summary_text.delta", "response.reasoning_text.delta":
		if event.Delta != "" {
			key := responsesOutputPartKey{OutputIndex: event.OutputIndex, PartIndex: event.SummaryIndex, PartType: responsesReasoningPartType(event.Type)}
			a.appendReasoningDelta(key, event.Delta)
		}
	case "response.reasoning_summary_text.done", "response.reasoning_text.done":
		key := responsesOutputPartKey{OutputIndex: event.OutputIndex, PartIndex: event.SummaryIndex, PartType: responsesReasoningPartType(event.Type)}
		a.appendReasoningAggregate(key, event.Text)
	}
}

func (a *BufferedResponseAccumulator) appendTextDelta(key responsesOutputPartKey, value string) {
	if value == "" {
		return
	}
	a.textParts[key] += value
	_, _ = a.text.WriteString(value)
}

func (a *BufferedResponseAccumulator) appendTextAggregate(key responsesOutputPartKey, value string) {
	appendBufferedAggregate(&a.text, a.textParts, key, value)
}

func (a *BufferedResponseAccumulator) appendReasoningDelta(key responsesOutputPartKey, value string) {
	if value == "" {
		return
	}
	a.reasoningParts[key] += value
	_, _ = a.reasoning.WriteString(value)
}

func (a *BufferedResponseAccumulator) appendReasoningAggregate(key responsesOutputPartKey, value string) {
	appendBufferedAggregate(&a.reasoning, a.reasoningParts, key, value)
}

func appendBufferedAggregate(
	output *strings.Builder,
	parts map[responsesOutputPartKey]string,
	key responsesOutputPartKey,
	aggregate string,
) {
	if output == nil || parts == nil || aggregate == "" {
		return
	}
	suffix, compatible := aggregateOutputSuffix(parts[key], aggregate)
	if !compatible {
		return
	}
	parts[key] = aggregate
	if suffix != "" {
		_, _ = output.WriteString(suffix)
	}
}

func (a *BufferedResponseAccumulator) processContentPart(event *ResponsesStreamEvent) {
	if event == nil || event.Part == nil {
		return
	}
	key := responsesOutputPartKey{OutputIndex: event.OutputIndex, PartIndex: event.ContentIndex, PartType: event.Part.Type}
	switch event.Part.Type {
	case "output_text":
		a.appendTextAggregate(key, event.Part.Text)
	case "refusal":
		a.appendTextAggregate(key, event.Part.Refusal)
	}
}

func (a *BufferedResponseAccumulator) processReasoningPart(event *ResponsesStreamEvent) {
	if event == nil || event.Part == nil || event.Part.Type != "summary_text" {
		return
	}
	key := responsesOutputPartKey{OutputIndex: event.OutputIndex, PartIndex: event.SummaryIndex, PartType: "reasoning_summary"}
	a.appendReasoningAggregate(key, event.Part.Text)
}

func (a *BufferedResponseAccumulator) processOutputItem(event *ResponsesStreamEvent) {
	if event == nil || event.Item == nil {
		return
	}
	item := event.Item
	switch item.Type {
	case "message":
		for index := range item.Content {
			part := &item.Content[index]
			key := responsesOutputPartKey{OutputIndex: event.OutputIndex, PartIndex: index, PartType: part.Type}
			switch part.Type {
			case "output_text":
				a.appendTextAggregate(key, part.Text)
			case "refusal":
				a.appendTextAggregate(key, part.Refusal)
			}
		}
	case "reasoning":
		for index := range item.Summary {
			part := &item.Summary[index]
			if part.Type == "summary_text" {
				key := responsesOutputPartKey{OutputIndex: event.OutputIndex, PartIndex: index, PartType: "reasoning_summary"}
				a.appendReasoningAggregate(key, part.Text)
			}
		}
	case "function_call", "custom_tool_call", "tool_search_call":
		idx := a.ensureFuncCall(event.OutputIndex, item.Type, item.CallID, item.Name)
		if idx >= 0 {
			a.appendFuncCallAggregate(idx, responsesToolCallArguments(item))
		}
	}
}

func (a *BufferedResponseAccumulator) ensureFuncCall(outputIndex int, kind, callID, name string) int {
	if idx, ok := a.outputIndexToFuncIdx[outputIndex]; ok {
		a.mergeFuncCallMetadata(idx, kind, callID, name)
		return idx
	}
	idx := len(a.funcCalls)
	a.outputIndexToFuncIdx[outputIndex] = idx
	a.funcCalls = append(a.funcCalls, bufferedFuncCall{Type: kind, CallID: callID, Name: name})
	return idx
}

func (a *BufferedResponseAccumulator) mergeFuncCallMetadata(idx int, kind, callID, name string) {
	if idx < 0 || idx >= len(a.funcCalls) {
		return
	}
	call := &a.funcCalls[idx]
	if call.Type == "" && kind != "" {
		call.Type = kind
	}
	if call.CallID == "" && callID != "" {
		call.CallID = callID
	}
	if call.Name == "" && name != "" {
		call.Name = name
	}
}

func (a *BufferedResponseAccumulator) appendFuncCallAggregate(idx int, aggregate string) {
	if idx < 0 || idx >= len(a.funcCalls) || aggregate == "" {
		return
	}
	call := &a.funcCalls[idx]
	suffix, compatible := aggregateOutputSuffix(call.Args.String(), aggregate)
	if compatible && suffix != "" {
		_, _ = call.Args.WriteString(suffix)
	}
}

func responsesToolCallTypeForEvent(eventType string) string {
	switch {
	case strings.Contains(eventType, "custom_tool_call"):
		return "custom_tool_call"
	case strings.Contains(eventType, "tool_search_call"):
		return "tool_search_call"
	default:
		return "function_call"
	}
}

// HasContent reports whether any content has been accumulated.
func (a *BufferedResponseAccumulator) HasContent() bool {
	return a.text.Len() > 0 || len(a.funcCalls) > 0 || a.reasoning.Len() > 0
}

// BuildOutput constructs a []ResponsesOutput from the accumulated delta
// content. The order matches what ResponsesToChatCompletions expects:
// reasoning → message → function_calls.
func (a *BufferedResponseAccumulator) BuildOutput() []ResponsesOutput {
	var out []ResponsesOutput

	if a.reasoning.Len() > 0 {
		out = append(out, ResponsesOutput{
			Type: "reasoning",
			Summary: []ResponsesSummary{{
				Type: "summary_text",
				Text: a.reasoning.String(),
			}},
		})
	}

	if a.text.Len() > 0 {
		out = append(out, ResponsesOutput{
			Type: "message",
			Role: "assistant",
			Content: []ResponsesContentPart{{
				Type: "output_text",
				Text: a.text.String(),
			}},
		})
	}

	for i := range a.funcCalls {
		kind := a.funcCalls[i].Type
		if !isResponsesToolCall(kind) {
			kind = "function_call"
		}
		item := ResponsesOutput{
			Type:   kind,
			CallID: a.funcCalls[i].CallID,
			Name:   a.funcCalls[i].Name,
		}
		if kind == "custom_tool_call" {
			item.Input = a.funcCalls[i].Args.String()
		} else {
			item.Arguments = a.funcCalls[i].Args.String()
		}
		out = append(out, item)
	}

	return out
}

// SupplementResponseOutput fills resp.Output from accumulated delta content
// when the terminal event delivered an empty output array. If resp.Output is
// already populated, this is a no-op (preserves backward compatibility).
func (a *BufferedResponseAccumulator) SupplementResponseOutput(resp *ResponsesResponse) {
	if resp == nil || len(resp.Output) > 0 {
		return
	}
	if !a.HasContent() {
		return
	}
	resp.Output = a.BuildOutput()
}
