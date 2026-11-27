//go:build unit

package service

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

// MODEL_QUALITY_INSPECTION: each question must reach the chosen model once,
// using OUTBOUND_IDENTITY's credential owner rather than an inherited identity.
func TestQualityProbeForwardsQuestionWithSingleAdaptiveRequest(t *testing.T) {
	account := adaptiveCNAccountTestAccount(301, PlatformDeepseek)
	account.Credentials["outbound_identity"] = map[string]any{"preset": "deepseek", "version": "1.2.3"}
	svc, upstream := adaptiveCNAccountTestService(account, adaptiveCNChatTestResponse())
	outcome, err := NewQualityProbe(svc).Run(context.Background(), account.ID, "deepseek-chat", "What is 17 * 19? Reply with 323.")
	require.NoError(t, err)
	require.Equal(t, QualityStatusPass, outcome.Status)
	require.Equal(t, "chat ok", outcome.Text)
	require.Len(t, upstream.requests, 1, "one question must not fan out into adaptive connectivity checks")
	req := upstream.requests[0]
	require.Equal(t, "http://chat.example/v1/chat/completions", req.URL.String())
	require.Equal(t, "What is 17 * 19? Reply with 323.", gjson.GetBytes(upstream.bodies[0], "messages.0.content").String())
	require.Equal(t, "deepseek-harness/1.2.3 (+https://github.com/deepseek-ai/deepseek-harness)", req.Header.Get("User-Agent"))
	require.Empty(t, req.Header.Get("Originator"))
	require.Empty(t, req.Header.Get("Version"))
}

func TestQualityProbeForwardsQuestionAcrossTextProtocols(t *testing.T) {
	for _, protocol := range []string{APIProtocolChatCompletions, APIProtocolAnthropic, APIProtocolResponses} {
		t.Run(protocol, func(t *testing.T) {
			account := adaptiveCNAccountTestAccount(301, PlatformDeepseek)
			account.Credentials["api_protocol"] = protocol
			var response *http.Response
			var path string
			switch protocol {
			case APIProtocolChatCompletions:
				response = adaptiveCNChatTestResponse()
				path = "messages.0.content"
			case APIProtocolAnthropic:
				response = adaptiveCNAnthropicTestResponse()
				path = "messages.0.content.0.text"
			default:
				response = adaptiveCNResponsesTestResponse()
				path = "input.0.content.0.text"
			}
			svc, upstream := adaptiveCNAccountTestService(account, response)
			_, err := NewQualityProbe(svc).Run(context.Background(), account.ID, "deepseek-chat", "Exact question to grade")
			require.NoError(t, err)
			require.Len(t, upstream.requests, 1)
			require.Equal(t, "Exact question to grade", gjson.GetBytes(upstream.bodies[0], path).String())
		})
	}
}

func TestQualityProbeClaudeAndOpenAIReceiveQuestion(t *testing.T) {
	for _, platform := range []string{PlatformAnthropic, PlatformOpenAI} {
		t.Run(platform, func(t *testing.T) {
			account := &Account{ID: 42, Platform: platform, Type: AccountTypeAPIKey, Status: StatusActive, Concurrency: 1, Credentials: map[string]any{"api_key": "test-key", "base_url": "http://chat.example"}}
			response := adaptiveCNAnthropicTestResponse()
			path := "messages.0.content.0.text"
			if platform == PlatformOpenAI {
				response = adaptiveCNResponsesTestResponse()
				path = "input.0.content.0.text"
			}
			svc, upstream := adaptiveCNAccountTestService(account, response)
			_, err := NewQualityProbe(svc).Run(context.Background(), account.ID, "model-one", "Question for the selected model")
			require.NoError(t, err)
			require.Len(t, upstream.requests, 1)
			require.Equal(t, "Question for the selected model", gjson.GetBytes(upstream.bodies[0], path).String())
			if platform == PlatformOpenAI {
				require.Equal(t, "codex_cli_rs/0.170.0 (Ubuntu 24.04; x86_64) xterm-256color", upstream.requests[0].Header.Get("User-Agent"))
				require.Equal(t, "codex_cli_rs", upstream.requests[0].Header.Get("Originator"))
				require.Equal(t, "0.170.0", upstream.requests[0].Header.Get("Version"))
			}
		})
	}
}

func TestQualityProbeBedrockQuestionIsSigned(t *testing.T) {
	account := &Account{ID: 42, Platform: PlatformAnthropic, Type: AccountTypeBedrock, Credentials: map[string]any{"aws_access_key_id": "test-key", "aws_secret_access_key": "test-secret", "aws_region": "us-east-1"}}
	response := &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{"content":[{"text":"323"}]}`))}
	svc, upstream := adaptiveCNAccountTestService(account, response)
	_, err := NewQualityProbe(svc).Run(context.Background(), account.ID, "claude-sonnet-4-5", "What is 17 * 19?")
	require.NoError(t, err)
	require.Len(t, upstream.requests, 1)
	require.Equal(t, "What is 17 * 19?", gjson.GetBytes(upstream.bodies[0], "messages.0.content.0.text").String())
	require.Contains(t, upstream.requests[0].Header.Get("Authorization"), "AWS4-HMAC-SHA256")
}

func TestQualityPromptDefaultAndBedrockPayload(t *testing.T) {
	// Ordinary connectivity/usage probes retain their documented empty-prompt default.
	for _, prompt := range []string{"", "A real quality question"} {
		expected := prompt
		if expected == "" {
			expected = "hi"
		}
		claude, err := createTestPayload("claude-sonnet-4-6", prompt)
		require.NoError(t, err)
		openai := createOpenAITestPayload("model-one", true, prompt)
		bedrock := buildBedrockTestPayload("claude-sonnet-4-6", prompt)
		for _, entry := range []struct {
			value map[string]any
			path  string
		}{{claude, "messages.0.content.0.text"}, {openai, "input.0.content.0.text"}, {bedrock, "messages.0.content.0.text"}} {
			raw, err := json.Marshal(entry.value)
			require.NoError(t, err)
			require.Equal(t, expected, gjson.GetBytes(raw, entry.path).String())
		}
	}
}

func TestQualityProbeGrokReceivesQuestion(t *testing.T) {
	account := &Account{ID: 42, Platform: PlatformGrok, Type: AccountTypeAPIKey, Status: StatusActive, Concurrency: 1, Credentials: map[string]any{"api_key": "test-key"}}
	svc, upstream := adaptiveCNAccountTestService(account, adaptiveCNResponsesTestResponse())
	_, err := NewQualityProbe(svc).Run(context.Background(), account.ID, "grok-4.3", "A specific question, not a quota probe")
	require.NoError(t, err)
	require.Len(t, upstream.requests, 1)
	require.Equal(t, "A specific question, not a quota probe", gjson.GetBytes(upstream.bodies[0], "input").String())
}
