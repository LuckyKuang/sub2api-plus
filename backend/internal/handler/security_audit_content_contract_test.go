package handler

import (
	"testing"

	"github.com/LuckyKuang/sub2api-plus/internal/securityaudit"
	"github.com/LuckyKuang/sub2api-plus/internal/service"
	"github.com/stretchr/testify/require"
)

func TestContentModerationUsesLatestUserTextWithoutInstructionContext(t *testing.T) {
	tests := []struct {
		name     string
		protocol string
		body     string
		want     string
		images   []string
	}{
		{
			name: "responses latest user", protocol: service.ContentModerationProtocolOpenAIResponses,
			body: `{"instructions":"audit instruction","tools":[{"type":"function","name":"lookup","description":"audit tool definition"}],` +
				`"input":[{"type":"message","role":"user","content":"older prompt"},{"type":"message","role":"user","content":"你好"}]}`,
			want: "你好",
		},
		{
			name: "responses legacy messages alias", protocol: service.ContentModerationProtocolOpenAIResponses,
			body: `{"messages":[{"role":"system","content":"legacy system"},{"role":"user","content":"legacy user"}]}`,
			want: "legacy user",
		},
		{
			name: "responses native input precedes aliases", protocol: service.ContentModerationProtocolOpenAIResponses,
			body: `{"input":"native user","messages":[{"role":"user","content":"ignored alias"}],"prompt":"ignored prompt"}`,
			want: "native user",
		},
		{
			name: "responses tool loop skipped", protocol: service.ContentModerationProtocolOpenAIResponses,
			body: `{"input":[{"type":"message","role":"user","content":"older prompt"},{"type":"function_call_output","call_id":"call_1","output":"current tool result"}]}`,
		},
		{
			name: "responses automation bootstrap is direct user text", protocol: service.ContentModerationProtocolOpenAIResponses,
			body: `{"model":"gpt-5","input":[{"type":"function_call_output","namespace":"codex_app","name":"automation_update","output":"Automation: Scheduled review\nAutomation ID: wiki\nAutomation memory: $CODEX_HOME/automations/wiki/memory.md\nLast run: never\n\nReview security"}]}`,
			want: "Automation: Scheduled review Automation ID: wiki Automation memory: $CODEX_HOME/automations/wiki/memory.md Last run: never Review security",
		},
		{
			name: "responses delegation bootstrap is direct user text", protocol: service.ContentModerationProtocolOpenAIResponses,
			body: `{"model":"gpt-5","input":[{"type":"function_call_output","namespace":"codex_app","name":"create_thread","output":"<codex_delegation><source_thread_id>thread-1</source_thread_id><input>review security</input></codex_delegation>"}]}`,
			want: "<codex_delegation><source_thread_id>thread-1</source_thread_id><input>review security</input></codex_delegation>",
		},
		{
			name: "responses assistant item skipped", protocol: service.ContentModerationProtocolOpenAIResponses,
			body: `{"input":[{"type":"message","role":"user","content":"older prompt"},{"type":"message","role":"assistant","content":"current assistant payload"}]}`,
		},
		{
			name: "responses prompt variables skipped", protocol: service.ContentModerationProtocolOpenAIResponses,
			body: `{"prompt":{"id":"pmpt_1","variables":{"plain":"reusable variable","image":{"type":"input_image","image_url":"https://example.test/prompt.png"}}}}`,
		},
		{
			name: "nested websocket user", protocol: service.ContentModerationProtocolOpenAIResponses,
			body: `{"type":"response.create","input":[],"response":{"input":[{"type":"message","role":"user","content":"nested content cannot be shadowed"}]}}`,
			want: "nested content cannot be shadowed",
		},
		{
			name: "chat latest user", protocol: service.ContentModerationProtocolOpenAIChat,
			body: `{"messages":[{"role":"system","content":"chat system context"},{"role":"user","content":"你好"}]}`,
			want: "你好",
		},
		{
			name: "chat user reasoning and reminder remain direct user text", protocol: service.ContentModerationProtocolOpenAIChat,
			body: `{"messages":[{"role":"assistant","reasoning_content":"assistant private"},{"role":"user","content":"<system-reminder>untrusted</system-reminder> question","reasoning_content":"user reasoning"}]}`,
			want: "<system-reminder>untrusted</system-reminder> question user reasoning",
		},
		{
			name: "alpha search structured semantics", protocol: "openai_alpha_search",
			body: `{"commands":{"search_query":[{"q":"security query"}],"mode":"deep"},"settings":{"region":"global"},"input":[{"type":"message","role":"user","content":"recent context"}]}`,
			want: `{"mode":"deep","search_query":[{"q":"security query"}]} {"region":"global"} [{"content":"recent context","role":"user","type":"message"}]`,
		},
		{
			name: "chat tool result skipped", protocol: service.ContentModerationProtocolOpenAIChat,
			body: `{"messages":[{"role":"user","content":"older"},{"role":"tool","content":"chat tool result"}]}`,
		},
		{
			name: "anthropic tool result skipped", protocol: service.ContentModerationProtocolAnthropicMessages,
			body: `{"system":"anthropic instruction","messages":[{"role":"user","content":[{"type":"tool_result","content":{"safe":false}}]}]}`,
		},
		{
			name: "gemini function response skipped", protocol: service.ContentModerationProtocolGemini,
			body: `{"systemInstruction":{"parts":[{"text":"gemini instruction"}]},"contents":[{"role":"user","parts":[{"functionResponse":{"response":{"safe":false}}}]}]}`,
		},
		{
			name: "live user image preserved", protocol: service.ContentModerationProtocolOpenAILive,
			body:   `{"type":"conversation.item.create","item":{"type":"message","role":"user","content":[{"type":"input_image","image_url":"https://example.test/live.png"}]}}`,
			images: []string{"https://example.test/live.png"},
		},
		{
			name: "live session instructions skipped", protocol: service.ContentModerationProtocolOpenAILive,
			body: `{"model":"gpt-live-test","instructions":"live instructions"}`,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			legacy := service.ExtractContentModerationInput(test.protocol, []byte(test.body))
			require.Equal(t, test.want, legacy.Text)
			if len(test.images) == 0 {
				require.Empty(t, legacy.Images)
			} else {
				require.Equal(t, test.images, legacy.Images)
			}
			require.NotContains(t, legacy.Text, "instruction")
			require.NotContains(t, legacy.Text, "tool definition")
			require.NotContains(t, legacy.Text, "system context")
		})
	}
}

func TestPromptAuditUsesConversationTextWithoutToolSchema(t *testing.T) {
	tests := []struct {
		name     string
		protocol string
		body     string
		full     []string
		latest   string
		omit     []string
		noFull   bool
		noLatest bool
	}{
		{
			name: "assistant text and tool schema are included", protocol: service.ContentModerationProtocolOpenAIResponses,
			body: `{"instructions":"audit instruction","tools":[{"type":"function","name":"lookup","description":"audit tool definition"}],` +
				`"input":[{"type":"message","role":"user","content":"older prompt"},{"type":"message","role":"assistant","content":"current assistant payload"}]}`,
			full:   []string{"older prompt", "current assistant payload", "audit instruction", "audit tool definition"},
			latest: "older prompt",
		},
		{
			name: "responses function result is prompt-audit conversation text", protocol: service.ContentModerationProtocolOpenAIResponses,
			body:   `{"input":[{"type":"message","role":"user","content":"older prompt"},{"type":"function_call_output","call_id":"call_1","output":"current tool result"}]}`,
			full:   []string{"older prompt", "current tool result"},
			latest: "older prompt",
		},
		{
			name: "responses automation bootstrap is prompt text", protocol: service.ContentModerationProtocolOpenAIResponses,
			body:   `{"model":"gpt-5","input":[{"type":"function_call_output","namespace":"codex_app","name":"automation_update","output":"Automation: Scheduled review\nAutomation ID: wiki\nAutomation memory: $CODEX_HOME/automations/wiki/memory.md\nLast run: never\n\nReview security"}]}`,
			full:   []string{"Automation: Scheduled review", "Review security"},
			latest: "Automation: Scheduled review\nAutomation ID: wiki\nAutomation memory: $CODEX_HOME/automations/wiki/memory.md\nLast run: never\n\nReview security",
		},
		{
			name: "responses delegation bootstrap is prompt text", protocol: service.ContentModerationProtocolOpenAIResponses,
			body:   `{"model":"gpt-5","input":[{"type":"function_call_output","namespace":"codex_tui","name":"send_message_to_thread","output":"<codex_delegation><source_thread_id>thread-1</source_thread_id><input>review security</input></codex_delegation>"}]}`,
			full:   []string{"<codex_delegation>", "review security"},
			latest: "<codex_delegation><source_thread_id>thread-1</source_thread_id><input>review security</input></codex_delegation>",
		},
		{
			name: "responses legacy messages and prompt aliases", protocol: service.ContentModerationProtocolOpenAIResponses,
			body:   `{"messages":[{"role":"user","content":"legacy prompt text"}],"prompt":"lower priority alias"}`,
			full:   []string{"legacy prompt text"},
			latest: "legacy prompt text",
			omit:   []string{"lower priority alias"},
		},
		{
			name: "chat user reasoning is prompt text", protocol: service.ContentModerationProtocolOpenAIChat,
			body:   `{"messages":[{"role":"assistant","reasoning_content":"assistant private"},{"role":"user","content":"question","reasoning_content":"user reasoning"}]}`,
			full:   []string{"question", "user reasoning", "assistant private"},
			latest: "question\n\nuser reasoning",
		},
		{
			name: "alpha search semantic fields", protocol: "openai_alpha_search",
			body:   `{"commands":{"search_query":[{"q":"security query"}],"mode":"deep"},"settings":{"region":"global"},"input":[{"type":"message","role":"user","content":"recent context"}]}`,
			full:   []string{"security query", "deep", "global", "recent context"},
			latest: "{\"mode\":\"deep\",\"search_query\":[{\"q\":\"security query\"}]}\n\n{\"region\":\"global\"}\n\n[{\"content\":\"recent context\",\"role\":\"user\",\"type\":\"message\"}]",
		},
		{
			name: "responses reusable prompt variables are included", protocol: service.ContentModerationProtocolOpenAIResponses,
			body:   `{"prompt":{"id":"pmpt_1","variables":{"plain":"reusable variable","typed":{"type":"input_text","text":"typed variable"}}}}`,
			full:   []string{"reusable variable", "typed variable"},
			latest: "reusable variable",
		},
		{
			name: "live session instructions and transcription context are included", protocol: service.ContentModerationProtocolOpenAILive,
			body: `{"model":"gpt-live-test","instructions":"live instructions",` +
				`"input_audio_transcription":{"model":"gpt-4o-transcribe","prompt":"legacy transcription context"},` +
				`"audio":{"input":{"transcription":{"model":"gpt-live-transcribe","prompt":"current transcription context","keywords":["premium plan","AC-42"]}}}}`,
			full:   []string{"live instructions", "legacy transcription context", "current transcription context", "premium plan", "AC-42"},
			latest: "live instructions",
		},
		{
			name: "client environment xml is stripped from user text", protocol: service.ContentModerationProtocolOpenAIResponses,
			body:   `{"input":[{"type":"message","role":"user","content":"继续"},{"type":"message","role":"user","content":"<environment_context><cwd>/Users/pontus</cwd><permission_profile type=\"managed\"></permission_profile></environment_context>"},{"type":"message","role":"user","content":"你能做什么？"}]}`,
			full:   []string{"继续", "你能做什么？"},
			latest: "继续\n\n你能做什么？",
			omit:   []string{"environment_context", "permission_profile", "/Users/pontus"},
		},
		{
			name: "chat tool-role and tool-call arguments are included", protocol: service.ContentModerationProtocolOpenAIChat,
			body:   `{"messages":[{"role":"system","content":"chat system context"},{"role":"user","content":"older"},{"role":"assistant","tool_calls":[{"function":{"arguments":"{\"secret\":true}"}}]},{"role":"tool","content":{"first":true}},{"role":"function","content":{"second":false}}]}`,
			full:   []string{"older", "chat system context", `"secret":true`, `{"first":true}`},
			latest: "older",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			full, err := securityaudit.ExtractPromptSnapshot(securityaudit.Request{
				Protocol: test.protocol,
				Body:     []byte(test.body),
			})
			if test.noFull {
				require.ErrorIs(t, err, securityaudit.ErrNoPromptText)
			} else {
				require.NoError(t, err)
				for _, expected := range test.full {
					require.Contains(t, full.ScanText, expected)
				}
				for _, omitted := range test.omit {
					require.NotContains(t, full.ScanText, omitted)
				}
			}
			latest, err := securityaudit.ExtractBlockingPromptSnapshot(securityaudit.Request{
				Protocol: test.protocol,
				Body:     []byte(test.body),
			}, true)
			if test.noLatest {
				require.ErrorIs(t, err, securityaudit.ErrNoPromptText)
			} else {
				require.NoError(t, err)
				require.Contains(t, latest.ScanText, test.latest)
				for _, omitted := range test.omit {
					require.NotContains(t, latest.ScanText, omitted)
				}
			}
		})
	}
}
