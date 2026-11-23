//go:build unit

package service

import (
	"context"
	"errors"
	"testing"

	"github.com/LuckyKuang/sub2api-plus/internal/pkg/apicompat"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestKimiChatDynamicToolsAcrossProtocols(t *testing.T) {
	gin.SetMode(gin.TestMode)
	body := []byte(`{
		"model":"kimi-k3.1-pro",
		"tools":[{"type":"function","function":{"name":"Lookup","parameters":{"type":"object"}}}],
		"tool_choice":"required",
		"messages":[
			{"role":"system","content":"Answer briefly."},
			{"role":"user","content":"Calculate 23 * 47.","tools":[{"type":"function","function":{"name":"IgnoreMe"}}]},
			{"role":"system","tools":[{"type":"function","function":{"name":"Calculator","parameters":{"type":"object","properties":{"expr":{"type":"string"}},"required":["expr"]}}}]}
		]
	}`)
	for _, protocol := range []string{APIProtocolChatCompletions, APIProtocolResponses, APIProtocolAnthropic} {
		t.Run(protocol, func(t *testing.T) {
			upstream := &httpUpstreamRecorder{err: errors.New("stop after capture")}
			svc := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: upstream}
			account := adaptiveProtocolTestAccount(PlatformKimi, map[string]any{protocol: "http://kimi.example"})
			account.Credentials["api_protocol"] = protocol
			c := adaptiveProtocolTestContext("/v1/chat/completions", body)
			_, err := svc.ForwardAsChatCompletions(context.Background(), c, account, body, "", "")
			require.Error(t, err)
			require.NotNil(t, upstream.lastReq)
			out := gjson.ParseBytes(upstream.lastBody)
			schema := gjson.GetBytes(body, "messages.2.tools.0.function.parameters").Raw
			switch protocol {
			case APIProtocolChatCompletions:
				require.JSONEq(t, string(body), string(upstream.lastBody))
			case APIProtocolResponses:
				require.JSONEq(t, `["Lookup","Calculator"]`, out.Get("tools.#.name").Raw)
				require.JSONEq(t, schema, out.Get("tools.1.parameters").Raw)
				require.Equal(t, "required", out.Get("tool_choice").String())
				require.Len(t, out.Get("input").Array(), 2)
			case APIProtocolAnthropic:
				require.JSONEq(t, `["Lookup","Calculator"]`, out.Get("tools.#.name").Raw)
				require.JSONEq(t, schema, out.Get("tools.1.input_schema").Raw)
				require.Equal(t, "any", out.Get("tool_choice.type").String())
				require.Equal(t, "Answer briefly.", out.Get("system").String())
			}
		})
	}
}

func TestKimiChatEmptyDynamicToolsAcrossBridgeProtocols(t *testing.T) {
	// A present null/empty declaration changes no tools and is removed from
	// the bridged conversation. Keep the current user on the actual wire.
	for _, tools := range []string{`[]`, `null`} {
		for _, protocol := range []string{APIProtocolResponses, APIProtocolAnthropic} {
			t.Run(tools+"/"+protocol, func(t *testing.T) {
				body := []byte(`{"model":"kimi-k3.1-pro","messages":[{"role":"user","content":"current question"},{"role":"system","tools":` + tools + `}]}`)
				upstream := &httpUpstreamRecorder{err: errors.New("stop after capture")}
				svc := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: upstream}
				account := adaptiveProtocolTestAccount(PlatformKimi, map[string]any{protocol: "http://kimi.example"})
				account.Credentials["api_protocol"] = protocol
				c := adaptiveProtocolTestContext("/v1/chat/completions", body)
				_, err := svc.ForwardAsChatCompletions(context.Background(), c, account, body, "", "")
				require.Error(t, err)
				require.NotNil(t, upstream.lastReq)
				out := gjson.ParseBytes(upstream.lastBody)
				// Both protocols permit string content; the bridge preserves
				// the client's plain string rather than adding a text-part array.
				if protocol == APIProtocolResponses {
					require.Len(t, out.Get("input").Array(), 1)
					require.Equal(t, "user", out.Get("input.0.role").String())
					require.Equal(t, "current question", out.Get("input.0.content").String())
				} else {
					require.Len(t, out.Get("messages").Array(), 1)
					require.Equal(t, "user", out.Get("messages.0.role").String())
					require.Equal(t, "current question", out.Get("messages.0.content").String())
				}
			})
		}
	}
}

func TestKimiChatContentNullIsNotToolOnlyDeclaration(t *testing.T) {
	// content is a RawMessage: explicit JSON null is present content, so the
	// bridge rejects it rather than treating it as an absent-content message.
	var req apicompat.ChatCompletionsRequest
	err := parseChatRequestForBridge([]byte(`{"messages":[{"role":"system","content":null,"tools":[]}]}`), PlatformKimi, &req)
	require.EqualError(t, err, "messages[0]: Kimi system tools cannot be used with content")
}
