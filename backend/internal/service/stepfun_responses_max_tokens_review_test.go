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
	"github.com/tidwall/gjson"
)

// Responses represents a token-limit stop as incomplete/max_output_tokens,
// with response.incomplete as the streaming terminal event. Unlike Messages
// input objects, Responses function arguments are strings and can retain the
// original truncated provider bytes without producing invalid response JSON.
// https://developers.openai.com/api/reference/resources/responses/streaming-events
func TestStepFunReviewResponsesPreservesMaxTokenToolTruncation(t *testing.T) {
	for _, stream := range []bool{false, true} {
		for _, tc := range []struct {
			name, finish, arguments string
			limited                 bool
		}{
			{name: "length_complete_object", finish: "length", arguments: `{"city":"Paris"}`, limited: true},
			{name: "length_truncated_object", finish: "length", arguments: `{"city":"Par`, limited: true},
			{name: "tool_calls_truncated_control", finish: "tool_calls", arguments: `{"city":"Par`},
			{name: "stop_truncated_control", finish: "stop", arguments: `{"city":"Par`},
		} {
			t.Run(fmt.Sprintf("stream=%t/%s", stream, tc.name), func(t *testing.T) {
				account := stepFunTestAccount(AccountTypeAPIKey, "cn")
				_, ctx := outboundIdentityTestSettings(t, emptyOutboundIdentitySettings())
				arguments, err := json.Marshal(tc.arguments)
				require.NoError(t, err)
				usage := `{"prompt_tokens":10,"completion_tokens":5,"total_tokens":15}`
				body := fmt.Sprintf(`{"id":"chat_step_response_length","model":"step-3.7-flash","choices":[{"index":0,"message":{"role":"assistant","content":null,"tool_calls":[{"id":"call_weather","type":"function","function":{"name":"weather","arguments":%s}}]},"finish_reason":%q}],"usage":%s}`, arguments, tc.finish, usage)
				contentType := "application/json"
				if stream {
					first, err := json.Marshal(tc.arguments[:8])
					require.NoError(t, err)
					last, err := json.Marshal(tc.arguments[8:])
					require.NoError(t, err)
					body = fmt.Sprintf("data: {\"id\":\"chat_step_response_length\",\"model\":\"step-3.7-flash\",\"choices\":[{\"index\":0,\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"call_weather\",\"type\":\"function\",\"function\":{\"name\":\"weather\",\"arguments\":%s}}]},\"finish_reason\":null}]}\n\n", first) +
						fmt.Sprintf("data: {\"choices\":[{\"index\":0,\"delta\":{\"tool_calls\":[{\"index\":0,\"function\":{\"arguments\":%s}}]},\"finish_reason\":null}]}\n\n", last) +
						fmt.Sprintf("data: {\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":%q}]}\n\n", tc.finish) +
						"data: " + `{"choices":[],"usage":` + usage + `}` + "\n\ndata: [DONE]\n\n"
					contentType = "text/event-stream"
				}
				upstream := &httpUpstreamRecorder{resp: &http.Response{StatusCode: http.StatusOK,
					Header: http.Header{"Content-Type": {contentType}}, Body: io.NopCloser(strings.NewReader(body))}}
				gateway := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: upstream, cnOAuthService: NewCNOAuthService(nil, nil, nil)}
				result, recorder, err := stepCapabilityForward(t, gateway, ctx, account, "responses", stepCapabilityRequest("responses", stream, 1))
				require.Len(t, upstream.requests, 1, "metered tool output must not be replayed")
				require.NotNil(t, result)
				require.Equal(t, 10, result.Usage.InputTokens)
				require.Equal(t, 5, result.Usage.OutputTokens)
				if !tc.limited {
					require.Error(t, err, "completed invalid tool arguments must not be silently discarded or finalized")
					var failover *UpstreamFailoverError
					require.False(t, errors.As(err, &failover), "do not replay a completed metered inference")
					require.True(t, result.UsageIncomplete)
					require.Equal(t, UsageSourcePartial, result.UsageSource)
					require.NotContains(t, recorder.Body.String(), `"type":"response.completed"`)
					require.NotContains(t, recorder.Body.String(), `"type":"response.incomplete"`)
					if !stream {
						require.Equal(t, http.StatusBadGateway, recorder.Code)
						require.True(t, json.Valid(recorder.Body.Bytes()))
					}
					return
				}
				require.NoError(t, err, "a token-limit stop must not be reported as provider failure")
				require.Equal(t, http.StatusOK, recorder.Code)
				require.False(t, result.UsageIncomplete, "a complete provider meter is not lost because the token budget was exhausted")
				require.Equal(t, UsageSourceUpstreamExact, result.UsageSource)
				assertResponse := func(response gjson.Result) {
					require.Equal(t, "response", response.Get("object").String())
					require.Equal(t, "incomplete", response.Get("status").String())
					require.Equal(t, "max_output_tokens", response.Get("incomplete_details.reason").String())
					require.Equal(t, int64(10), response.Get("usage.input_tokens").Int())
					require.Equal(t, int64(5), response.Get("usage.output_tokens").Int())
					require.Len(t, response.Get("output").Array(), 1, "the original tool must remain in output")
					tool := response.Get("output.0")
					require.Equal(t, "function_call", tool.Get("type").String())
					require.Equal(t, "call_weather", tool.Get("call_id").String())
					require.Equal(t, "weather", tool.Get("name").String())
					require.Equal(t, tc.arguments, tool.Get("arguments").String(), "Responses can preserve partial arguments as a JSON string")
					if tc.name == "length_truncated_object" {
						require.Equal(t, "incomplete", tool.Get("status").String(), "a truncated tool must not become an executable completed item")
					}
				}
				if !stream {
					require.True(t, json.Valid(recorder.Body.Bytes()))
					assertResponse(gjson.Parse(recorder.Body.String()))
					return
				}
				terminals, done := 0, 0
				var fragments []string
				for _, line := range strings.Split(recorder.Body.String(), "\n") {
					if !strings.HasPrefix(line, "data:") {
						continue
					}
					data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
					if data == "[DONE]" {
						done++
						require.Equal(t, 1, terminals)
						continue
					}
					require.True(t, json.Valid([]byte(data)))
					event := gjson.Parse(data)
					switch event.Get("type").String() {
					case "response.function_call_arguments.delta":
						fragments = append(fragments, event.Get("delta").String())
					case "response.output_item.done":
						if tc.name == "length_truncated_object" && event.Get("item.type").String() == "function_call" {
							require.Equal(t, "incomplete", event.Get("item.status").String())
						}
					case "response.incomplete":
						terminals++
						assertResponse(event.Get("response"))
					case "response.completed", "response.failed", "error":
						t.Fatalf("token-limit termination has the wrong event: %s", data)
					}
				}
				require.Equal(t, 1, terminals)
				require.Equal(t, 1, done)
				require.Equal(t, []string{tc.arguments[:8], tc.arguments[8:]}, fragments)
			})
		}
	}
}
