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

// A provider length stop is successful token-limit termination, including an
// incomplete tool input. Messages clients must receive max_tokens so that they
// can retry with a larger budget, rather than treating this as provider failure.
// https://platform.claude.com/docs/en/build-with-claude/handling-stop-reasons#max_tokens
// For buffered adaptation, the gateway preserves the tool ID/name and represents
// incomplete input as {} because invalid RawMessage cannot form a JSON response.
// Streaming preserves the original partial_json bytes and the max_tokens stop.
func TestStepFunReviewMessagesPreservesMaxTokenToolTruncation(t *testing.T) {
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
				body := fmt.Sprintf(`{"id":"chat_step_length_tool","model":"step-3.7-flash","choices":[{"index":0,"message":{"role":"assistant","content":null,"tool_calls":[{"id":"call_weather","type":"function","function":{"name":"weather","arguments":%s}}]},"finish_reason":%q}],"usage":%s}`, arguments, tc.finish, usage)
				contentType := "application/json"
				if stream {
					// Two argument deltas make retention of the original fragments
					// observable, rather than accepting a synthetic replacement.
					first, err := json.Marshal(tc.arguments[:8])
					require.NoError(t, err)
					last, err := json.Marshal(tc.arguments[8:])
					require.NoError(t, err)
					body = fmt.Sprintf("data: {\"id\":\"chat_step_length_tool\",\"model\":\"step-3.7-flash\",\"choices\":[{\"index\":0,\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"call_weather\",\"type\":\"function\",\"function\":{\"name\":\"weather\",\"arguments\":%s}}]},\"finish_reason\":null}]}\n\n", first) +
						fmt.Sprintf("data: {\"choices\":[{\"index\":0,\"delta\":{\"tool_calls\":[{\"index\":0,\"function\":{\"arguments\":%s}}]},\"finish_reason\":null}]}\n\n", last) +
						fmt.Sprintf("data: {\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":%q}]}\n\n", tc.finish) +
						"data: " + `{"choices":[],"usage":` + usage + `}` + "\n\ndata: [DONE]\n\n"
					contentType = "text/event-stream"
				}
				upstream := &httpUpstreamRecorder{resp: &http.Response{StatusCode: http.StatusOK,
					Header: http.Header{"Content-Type": {contentType}}, Body: io.NopCloser(strings.NewReader(body))}}
				gateway := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: upstream, cnOAuthService: NewCNOAuthService(nil, nil, nil)}
				result, recorder, err := stepCapabilityForward(t, gateway, ctx, account, "messages", stepCapabilityRequest("messages", stream, 1))
				require.Len(t, upstream.requests, 1, "a metered inference must not be replayed")
				require.NotNil(t, result)
				require.Equal(t, 10, result.Usage.InputTokens)
				require.Equal(t, 5, result.Usage.OutputTokens)
				if !tc.limited {
					require.Error(t, err, "completed tool input remains subject to object validation")
					var failover *UpstreamFailoverError
					require.False(t, errors.As(err, &failover))
					require.True(t, result.UsageIncomplete)
					require.Equal(t, UsageSourcePartial, result.UsageSource)
					require.NotContains(t, recorder.Body.String(), `"type":"message_stop"`)
					if !stream {
						require.Equal(t, http.StatusBadGateway, recorder.Code)
						require.True(t, json.Valid(recorder.Body.Bytes()))
					}
					return
				}
				require.NoError(t, err, "token-limit termination is not a provider failure")
				require.Equal(t, http.StatusOK, recorder.Code)
				require.False(t, result.UsageIncomplete)
				require.Equal(t, UsageSourceUpstreamExact, result.UsageSource)
				output := stepCapabilityReadOutput(t, "messages", stream, recorder.Body.String())
				require.Len(t, output.Tools, 1)
				for _, tool := range output.Tools {
					require.Equal(t, "call_weather", tool.ID)
					require.Equal(t, "weather", tool.Name)
					if stream {
						require.Equal(t, tc.arguments, tool.Arguments, "streamed tool fragments must not be rewritten")
					} else if tc.name == "length_truncated_object" {
						require.JSONEq(t, `{}`, tool.Arguments, "buffered truncated input requires safe JSON representation")
					} else {
						require.JSONEq(t, tc.arguments, tool.Arguments)
					}
				}
				if !stream {
					response := gjson.Parse(recorder.Body.String())
					require.Equal(t, "message", response.Get("type").String())
					require.Equal(t, "max_tokens", response.Get("stop_reason").String())
					require.Equal(t, int64(10), response.Get("usage.input_tokens").Int())
					require.Equal(t, int64(5), response.Get("usage.output_tokens").Int())
					return
				}
				starts, blockStops, deltas, stops := 0, 0, 0, 0
				var fragments []string
				for _, line := range strings.Split(recorder.Body.String(), "\n") {
					if !strings.HasPrefix(line, "data:") {
						continue
					}
					data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
					require.True(t, json.Valid([]byte(data)), "each Messages SSE payload must remain valid JSON")
					event := gjson.Parse(data)
					switch event.Get("type").String() {
					case "message_start":
						starts++
					case "content_block_delta":
						if event.Get("delta.type").String() == "input_json_delta" {
							fragments = append(fragments, event.Get("delta.partial_json").String())
						}
					case "content_block_stop":
						blockStops++
					case "message_delta":
						deltas++
						require.Equal(t, 1, blockStops)
						require.Equal(t, "max_tokens", event.Get("delta.stop_reason").String())
						require.Equal(t, int64(10), event.Get("usage.input_tokens").Int())
						require.Equal(t, int64(5), event.Get("usage.output_tokens").Int())
					case "message_stop":
						stops++
						require.Equal(t, 1, deltas)
					case "error":
						t.Fatalf("max_tokens must not produce an error: %s", data)
					}
				}
				require.Equal(t, 1, starts)
				require.Equal(t, 1, blockStops)
				require.Equal(t, 1, deltas)
				require.Equal(t, 1, stops)
				require.Equal(t, []string{tc.arguments[:8], tc.arguments[8:]}, fragments)
			})
		}
	}
}
