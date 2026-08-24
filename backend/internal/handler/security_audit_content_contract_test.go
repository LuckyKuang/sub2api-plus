package handler

import (
	"strings"
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
			name: "responses tool loop skipped", protocol: service.ContentModerationProtocolOpenAIResponses,
			body: `{"input":[{"type":"message","role":"user","content":"older prompt"},{"type":"function_call_output","call_id":"call_1","output":"current tool result"}]}`,
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

func TestPromptAuditStillCoversInstructionsAndCurrentClientContent(t *testing.T) {
	tests := []struct {
		name     string
		protocol string
		body     string
		current  []string
	}{
		{
			name: "current assistant role plus context", protocol: service.ContentModerationProtocolOpenAIResponses,
			body: `{"instructions":"audit instruction","tools":[{"type":"function","name":"lookup","description":"audit tool definition"}],` +
				`"input":[{"type":"message","role":"user","content":"older prompt"},{"type":"message","role":"assistant","content":"current assistant payload"}]}`,
			current: []string{"current assistant payload", "audit instruction", "audit tool definition"},
		},
		{
			name: "responses function result", protocol: service.ContentModerationProtocolOpenAIResponses,
			body:    `{"input":[{"type":"message","role":"user","content":"older prompt"},{"type":"function_call_output","call_id":"call_1","output":"current tool result"}]}`,
			current: []string{"current tool result"},
		},
		{
			name: "responses reusable prompt variables", protocol: service.ContentModerationProtocolOpenAIResponses,
			body:    `{"prompt":{"id":"pmpt_1","variables":{"plain":"reusable variable","typed":{"type":"input_text","text":"typed variable"}}}}`,
			current: []string{"reusable variable", "typed variable"},
		},
		{
			name: "live initial session", protocol: service.ContentModerationProtocolOpenAILive,
			body: `{"model":"gpt-live-test","instructions":"live instructions",` +
				`"input_audio_transcription":{"model":"gpt-4o-transcribe","prompt":"legacy transcription context"},` +
				`"audio":{"input":{"transcription":{"model":"gpt-live-transcribe","prompt":"current transcription context","keywords":["premium plan","AC-42"]}}}}`,
			current: []string{"live instructions", "legacy transcription context", "current transcription context", "premium plan", "AC-42"},
		},
		{
			name: "chat parallel structured tool results and system context", protocol: service.ContentModerationProtocolOpenAIChat,
			body:    `{"messages":[{"role":"system","content":"chat system context"},{"role":"user","content":"older"},{"role":"assistant","tool_calls":[{"function":{"arguments":"{}"}}]},{"role":"tool","content":{"first":true}},{"role":"function","content":{"second":false}}]}`,
			current: []string{`{"first":true}`, `{"second":false}`, "chat system context"},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
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
				require.Contains(t, full.ScanText, expected)
				require.Contains(t, latest.ScanText, expected)
			}
			require.True(t, strings.HasPrefix(latest.ScanText, test.current[0]))
		})
	}
}
