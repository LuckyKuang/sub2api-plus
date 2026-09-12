//go:build unit

package service

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/pkg/apicompat"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestCCScanRequiresActualCompletionForUsage(t *testing.T) {
	prefix := "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"hello\"}}],\"usage\":{\"prompt_tokens\":2,\"completion_tokens\":10}}\n\n"
	for _, tc := range []struct {
		name, tail string
		incomplete bool
	}{
		{"truncated", "", true},
		{"done", "data: [DONE]\n\n", false},
		{"finish without done", "data: {\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}]}\n\n", true},
		{"error then done", "data: {\"error\":{\"type\":\"overloaded_error\"}}\n\ndata: [DONE]\n\n", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig()}
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
			resp := &http.Response{Body: io.NopCloser(strings.NewReader(prefix + tc.tail))}
			emitted := ""
			scan := svc.scanCCStream(c, resp, "test", "synthetic", time.Now(), func(chunk *apicompat.ChatCompletionsChunk) {
				for _, choice := range chunk.Choices {
					if choice.Delta.Content != nil {
						emitted += *choice.Delta.Content
					}
				}
			})
			require.NoError(t, scan.Err)
			require.Equal(t, tc.incomplete, scan.usageIncomplete())
			require.Equal(t, "hello", emitted)
			require.Equal(t, 10, scan.Usage.OutputTokens)
		})
	}
}

func TestCCFallbacksDoNotTreatErrorThenDoneAsCompleteUsage(t *testing.T) {
	for _, endpoint := range []string{"messages", "responses"} {
		t.Run(endpoint, func(t *testing.T) {
			body := []byte(`{"model":"gpt-5.1","stream":true,"max_tokens":32,"messages":[{"role":"user","content":"hi"}],"input":"hi"}`)
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/"+endpoint, strings.NewReader(string(body)))
			upstream := &httpUpstreamRecorder{resp: &http.Response{StatusCode: http.StatusOK,
				Header: http.Header{"Content-Type": []string{"text/event-stream"}},
				Body:   io.NopCloser(strings.NewReader("data: {\"id\":\"synthetic\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"hello\"}}],\"usage\":{\"prompt_tokens\":2,\"completion_tokens\":10}}\n\ndata: {\"error\":{\"type\":\"overloaded_error\"}}\n\ndata: [DONE]\n\n")),
			}}
			svc := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: upstream}
			var result *OpenAIForwardResult
			var err error
			if endpoint == "messages" {
				result, err = svc.ForwardAsAnthropic(context.Background(), c, forceChatMessagesFallbackAccount(), body, "", "")
			} else {
				result, err = svc.Forward(context.Background(), c, forceChatResponsesFallbackAccount(), body)
			}
			require.NoError(t, err)
			require.NotNil(t, result)
			require.False(t, result.UsageComplete())
			require.NotNil(t, result.FirstTokenMs)
			require.Equal(t, 10, result.Usage.OutputTokens)
		})
	}
}
