//go:build unit

package service

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// The Messages streaming contract requires the finished tool_use.input to be
// an object: https://platform.claude.com/docs/en/build-with-claude/streaming
// (Input JSON delta). An upstream finish reason and DONE cannot make damaged
// function arguments a valid completed Messages response. Preserve measured
// consumption, but do not finalize/replay the failed inference.
func TestStepFunReviewMessagesRejectsInvalidCompletedToolInput(t *testing.T) {
	for _, stream := range []bool{false, true} {
		for _, tc := range []struct {
			name, arguments string
			valid           bool
		}{
			{name: "object", arguments: `{"city":"Paris"}`, valid: true},
			{name: "empty_arguments", valid: true},
			{name: "whitespace_arguments", arguments: " \t ", valid: true},
			{name: "truncated_json", arguments: `{"city":`},
			{name: "trailing_json", arguments: `{"city":"Paris"}{}`},
			{name: "array_input", arguments: `["Paris"]`},
			{name: "scalar_input", arguments: `"Paris"`},
			{name: "null_input", arguments: `null`},
		} {
			t.Run(fmt.Sprintf("stream=%t/%s", stream, tc.name), func(t *testing.T) {
				account := stepFunTestAccount(AccountTypeAPIKey, "cn")
				_, ctx := outboundIdentityTestSettings(t, emptyOutboundIdentitySettings())
				arguments, err := json.Marshal(tc.arguments)
				require.NoError(t, err)
				tool := `{"id":"call_weather","type":"function","function":{"name":"weather","arguments":` + string(arguments) + `}}`
				usage := `{"prompt_tokens":10,"completion_tokens":5,"total_tokens":15}`
				body := `{"id":"chat_step_invalid_tool","model":"step-3.7-flash","choices":[{"index":0,"message":{"role":"assistant","content":null,"tool_calls":[` + tool + `]},"finish_reason":"tool_calls"}],"usage":` + usage + `}`
				contentType := "application/json"
				if stream {
					tool = strings.Replace(tool, `{"id":`, `{"index":0,"id":`, 1)
					body = "data: " + `{"id":"chat_step_invalid_tool","model":"step-3.7-flash","choices":[{"index":0,"delta":{"tool_calls":[` + tool + `]},"finish_reason":null}]}` + "\n\n" +
						"data: " + `{"choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}]}` + "\n\n" +
						"data: " + `{"choices":[],"usage":` + usage + `}` + "\n\ndata: [DONE]\n\n"
					contentType = "text/event-stream"
				}
				upstream := &httpUpstreamRecorder{resp: &http.Response{StatusCode: http.StatusOK,
					Header: http.Header{"Content-Type": {contentType}}, Body: io.NopCloser(strings.NewReader(body))}}
				gateway := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: upstream, cnOAuthService: NewCNOAuthService(nil, nil, nil)}
				result, recorder, err := stepCapabilityForward(t, gateway, ctx, account, "messages", stepCapabilityRequest("messages", stream, 1))
				require.Len(t, upstream.requests, 1)
				require.NotNil(t, result, "the actual provider token observation must survive protocol rejection")
				require.Equal(t, 10, result.Usage.InputTokens)
				require.Equal(t, 5, result.Usage.OutputTokens)
				if tc.valid {
					require.NoError(t, err)
					require.False(t, result.UsageIncomplete)
					require.Equal(t, UsageSourceUpstreamExact, result.UsageSource)
					out := stepCapabilityReadOutput(t, "messages", stream, recorder.Body.String())
					require.True(t, out.Terminal)
					require.Len(t, out.Tools, 1)
					wantInput := tc.arguments
					if strings.TrimSpace(wantInput) == "" {
						wantInput = `{}`
					}
					for _, tool := range out.Tools {
						require.Equal(t, "call_weather", tool.ID)
						require.Equal(t, "weather", tool.Name)
						require.JSONEq(t, wantInput, tool.Arguments)
					}
					return
				}
				require.Error(t, err, "DONE and finish_reason cannot validate damaged tool input")
				var failover *UpstreamFailoverError
				require.False(t, errors.As(err, &failover), "do not replay a completed and metered inference")
				require.True(t, result.UsageIncomplete)
				require.Equal(t, UsageSourcePartial, result.UsageSource)
				require.NotContains(t, recorder.Body.String(), `"type":"message_stop"`)
				require.NotContains(t, recorder.Body.String(), `"stop_reason":"tool_use"`)
				if !stream {
					require.Equal(t, http.StatusBadGateway, recorder.Code, "buffered protocol failures must not return a successful HTTP response")
					require.True(t, json.Valid(recorder.Body.Bytes()))
				}
			})
		}
	}
}
