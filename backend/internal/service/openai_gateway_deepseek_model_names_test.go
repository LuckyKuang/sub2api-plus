//go:build unit

package service

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestDeepSeekFlashNamesForwardUnchanged(t *testing.T) {
	gin.SetMode(gin.TestMode)
	chatReply := `{"id":"chat_1","object":"chat.completion","model":"deepseek-v4.1-flash","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1}}`
	responsesReply := `{"id":"resp_1","object":"response","status":"completed","model":"deepseek-v4.1-flash","output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"ok"}]}],"usage":{"input_tokens":1,"output_tokens":1}}`
	messagesReply := `{"id":"msg_1","type":"message","role":"assistant","model":"deepseek-v4.1-flash","content":[{"type":"text","text":"ok"}],"stop_reason":"end_turn","usage":{"input_tokens":1,"output_tokens":1}}`
	for _, tc := range []struct {
		name         string
		path         string
		protocol     string
		payload      string
		response     string
		upstreamPath string
	}{
		{"chat", "/v1/chat/completions", APIProtocolChatCompletions, `{"model":%q,"messages":[{"role":"user","content":"hi"}],"stream":false}`, chatReply, "/v1/chat/completions"},
		{"responses native", "/v1/responses", APIProtocolResponses, `{"model":%q,"input":"hi","stream":false}`, responsesReply, "/responses"},
		{"responses via chat", "/v1/responses", APIProtocolChatCompletions, `{"model":%q,"input":"hi","stream":false}`, chatReply, "/v1/chat/completions"},
		{"messages native", "/v1/messages", APIProtocolAnthropic, `{"model":%q,"max_tokens":32,"messages":[{"role":"user","content":"hi"}],"stream":false}`, messagesReply, "/v1/messages"},
		{"messages via chat", "/v1/messages", APIProtocolChatCompletions, `{"model":%q,"max_tokens":32,"messages":[{"role":"user","content":"hi"}],"stream":false}`, chatReply, "/v1/chat/completions"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, model := range []string{"deepseek-flash", "deepseek-v4.1-flash", "deepseek-v4-flash-vision-exp", "deepseek-v4-flash"} {
				t.Run(model, func(t *testing.T) {
					account := &Account{
						ID:       101,
						Platform: PlatformDeepseek,
						Type:     AccountTypeAPIKey,
						Credentials: map[string]any{
							"api_key":      "test-key",
							"api_protocol": tc.protocol,
							"base_url":     "https://relay.example",
						},
					}
					// A selectable name must reach the scheduler without requiring a
					// mapping, and the actual outbound body must retain that name.
					require.True(t, account.IsModelSupported(model))
					upstream := &httpUpstreamRecorder{resp: &http.Response{
						StatusCode: http.StatusOK,
						Header:     http.Header{"Content-Type": []string{"application/json"}},
						Body:       io.NopCloser(strings.NewReader(tc.response)),
					}}
					svc := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: upstream}
					body := []byte(fmt.Sprintf(tc.payload, model))
					c := adaptiveProtocolTestContext(tc.path, body)
					var result *OpenAIForwardResult
					var err error
					switch tc.path {
					case "/v1/chat/completions":
						result, err = svc.forwardAsRawChatCompletions(context.Background(), c, account, body, "")
					case "/v1/responses":
						result, err = svc.Forward(context.Background(), c, account, body)
					case "/v1/messages":
						result, err = svc.ForwardAsAnthropic(context.Background(), c, account, body, "", "")
					}
					require.NoError(t, err)
					require.NotNil(t, result)
					require.NotNil(t, upstream.lastReq)
					require.Equal(t, tc.upstreamPath, upstream.lastReq.URL.Path)
					require.Equal(t, model, gjson.GetBytes(upstream.lastBody, "model").String())
					require.Equal(t, model, result.UpstreamModel)
				})
			}
		})
	}
}
