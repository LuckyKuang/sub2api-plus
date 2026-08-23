package handler

import (
	"strings"
	"testing"

	"github.com/LuckyKuang/sub2api-plus/internal/securityaudit"
	"github.com/LuckyKuang/sub2api-plus/internal/service"
	"github.com/stretchr/testify/require"
)

func TestSecurityAuditEnginesShareCanonicalContentCoverage(t *testing.T) {
	tests := []struct {
		name     string
		protocol string
		body     string
		current  []string
	}{
		{
			name: "responses function result", protocol: service.ContentModerationProtocolOpenAIResponses,
			body:    `{"input":[{"type":"message","role":"user","content":"older prompt"},{"type":"function_call_output","call_id":"call_1","output":"current tool result"}]}`,
			current: []string{"current tool result"},
		},
		{
			name: "current assistant role plus context", protocol: service.ContentModerationProtocolOpenAIResponses,
			body: `{"instructions":"audit instruction","tools":[{"type":"function","name":"lookup","description":"audit tool definition"}],` +
				`"input":[{"type":"message","role":"user","content":"older prompt"},{"type":"message","role":"assistant","content":"current assistant payload"}]}`,
			current: []string{"current assistant payload", "audit instruction", "audit tool definition"},
		},
		{
			name: "responses custom and search results", protocol: service.ContentModerationProtocolOpenAIResponses,
			body:    `{"input":[{"type":"custom_tool_call_output","output":{"status":"ok"}},{"type":"tool_search_output","output":"search evidence"}]}`,
			current: []string{`{"status":"ok"}`, "search evidence"},
		},
		{
			name: "responses reusable prompt variables", protocol: service.ContentModerationProtocolOpenAIResponses,
			body:    `{"prompt":{"id":"pmpt_1","variables":{"plain":"prompt variable","typed":{"type":"input_text","text":"typed variable"}}}}`,
			current: []string{"prompt variable", "typed variable"},
		},
		{
			name: "responses official shell outputs", protocol: service.ContentModerationProtocolOpenAIResponses,
			body:    `{"input":[{"type":"local_shell_call_output","call_id":"l1","status":"completed","output":"local output"},{"type":"shell_call_output","call_id":"s1","status":"completed","output":[{"stdout":"shell output","stderr":"","outcome":{"type":"exit","exit_code":0}}]},{"type":"apply_patch_call_output","call_id":"p1","status":"failed","output":"patch output"}]}`,
			current: []string{"local output", "shell output", "patch output"},
		},
		{
			name: "responses official tool search result", protocol: service.ContentModerationProtocolOpenAIResponses,
			body:    `{"input":[{"type":"tool_search_output","execution":"client","call_id":"t1","status":"completed","tools":[{"type":"function","name":"lookup","description":"dynamic tool definition"}]}]}`,
			current: []string{`[{"description":"dynamic tool definition","name":"lookup","type":"function"}]`},
		},
		{
			name: "responses mcp and programmatic tool calling", protocol: service.ContentModerationProtocolOpenAIResponses,
			body:    `{"input":[{"type":"mcp_call","id":"m1","status":"completed","server_label":"docs","name":"lookup","arguments":"{\"q\":\"audit\"}","output":"mcp output","error":null},{"type":"program_output","call_id":"p1","status":"completed","result":"program output"}]}`,
			current: []string{`{"q":"audit"}`, "mcp output", "program output"},
		},
		{
			name: "responses mcp result", protocol: service.ContentModerationProtocolOpenAIResponses,
			body:    `{"input":[{"type":"reasoning","encrypted_content":"gAAAAAB","summary":[]},{"type":"mcp_tool_call","arguments":{"q":"docs"}},{"type":"mcp_tool_call_output","output":{"result":"found"}}]}`,
			current: []string{`{"result":"found"}`},
		},
		{
			name: "nested websocket", protocol: service.ContentModerationProtocolOpenAIResponses,
			body:    `{"type":"response.create","model":"gpt-test","response":{"input":"nested ws content"}}`,
			current: []string{"nested ws content"},
		},
		{
			name: "top-level input cannot shadow nested websocket", protocol: service.ContentModerationProtocolOpenAIResponses,
			body:    `{"type":"response.create","input":[],"response":{"input":"nested content cannot be shadowed"}}`,
			current: []string{"nested content cannot be shadowed"},
		},
		{
			name: "alpha search", protocol: "openai_alpha_search",
			body:    `{"commands":{"search_query":[{"q":"security query"}]},"input":[{"type":"message","role":"user","content":"recent search context"}]}`,
			current: []string{"security query", "recent search context"},
		},
		{
			name: "responses top-level type does not bypass HTTP content", protocol: service.ContentModerationProtocolOpenAIResponses,
			body:    `{"type":"future.control","input":"typed HTTP content"}`,
			current: []string{"typed HTTP content"},
		},
		{
			name: "live initial session", protocol: service.ContentModerationProtocolOpenAILive,
			body: `{"model":"gpt-live-test","instructions":"live instructions",` +
				`"input_audio_transcription":{"model":"gpt-4o-transcribe","prompt":"legacy transcription context"},` +
				`"audio":{"input":{"transcription":{"model":"gpt-live-transcribe","prompt":"current transcription context","keywords":["premium plan","AC-42"]}}}}`,
			current: []string{"live instructions", "legacy transcription context", "current transcription context", "premium plan", "AC-42"},
		},
		{
			name: "live sideband session update", protocol: "openai_live",
			body:    `{"type":"session.update","session":{"instructions":"sideband instruction","tools":[{"name":"lookup","description":"sideband tool policy"}],"audio":{"input":{"transcription":{"model":"gpt-live-transcribe","prompt":"sideband transcription context"}}}}}`,
			current: []string{"sideband instruction", "sideband tool policy", "sideband transcription context"},
		},
		{
			name: "live sideband tool result", protocol: "openai_live",
			body:    `{"type":"conversation.item.create","item":{"type":"function_call_output","output":"sideband tool result"}}`,
			current: []string{"sideband tool result"},
		},
		{
			name: "embeddings array", protocol: "openai_embeddings",
			body:    `{"input":["embedding one","embedding two"]}`,
			current: []string{"embedding one", "embedding two"},
		},
		{
			name: "chat tool result", protocol: service.ContentModerationProtocolOpenAIChat,
			body:    `{"messages":[{"role":"user","content":"older"},{"role":"tool","content":"chat tool result"}]}`,
			current: []string{"chat tool result"},
		},
		{
			name: "chat parallel structured tool results and system context", protocol: service.ContentModerationProtocolOpenAIChat,
			body:    `{"messages":[{"role":"system","content":"chat system context"},{"role":"user","content":"older"},{"role":"assistant","tool_calls":[{"function":{"arguments":"{}"}}]},{"role":"tool","content":{"first":true}},{"role":"function","content":{"second":false}}]}`,
			current: []string{`{"first":true}`, `{"second":false}`, "chat system context"},
		},
		{
			name: "anthropic tool result", protocol: service.ContentModerationProtocolAnthropicMessages,
			body:    `{"messages":[{"role":"user","content":[{"type":"tool_result","content":{"safe":false}}]}]}`,
			current: []string{`{"safe":false}`},
		},
		{
			name: "gemini function response", protocol: service.ContentModerationProtocolGemini,
			body:    `{"contents":[{"role":"user","parts":[{"functionResponse":{"response":{"safe":false}}}]}]}`,
			current: []string{`{"safe":false}`},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			legacy := service.ExtractContentModerationInput(test.protocol, []byte(test.body))
			require.NotEmpty(t, legacy.Text)

			full, err := securityaudit.ExtractPromptSnapshot(securityaudit.Request{
				Protocol: test.protocol,
				Body:     []byte(test.body),
			})
			require.NoError(t, err)
			latest, err := securityaudit.ExtractBlockingPromptSnapshot(securityaudit.Request{
				Protocol: test.protocol,
				Body:     []byte(test.body),
			}, true)
			require.NoError(t, err)

			for _, expected := range test.current {
				require.Contains(t, legacy.Text, expected)
				require.Contains(t, full.ScanText, expected)
				require.Contains(t, latest.ScanText, expected)
			}
			require.True(t, strings.HasPrefix(latest.ScanText, test.current[0]))
		})
	}
}
