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

func TestNativeAnthropicTimingAcrossPlatformsAndAdapters(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, platform := range []string{PlatformKimi, PlatformZhipu, PlatformDeepseek} {
		for _, endpoint := range []string{"messages", "chat/completions", "responses"} {
			t.Run(platform+"/"+endpoint, func(t *testing.T) {
				svc := newNativeAnthropicHangTestService(5)
				reader, writer := io.Pipe()
				defer reader.Close()
				finished := make(chan struct{})
				go func() {
					defer close(finished)
					defer writer.Close()
					parts := strings.SplitN(miniAnthropicSSEStream(), "event: content_block_delta", 2)
					_, _ = io.WriteString(writer, parts[0])
					time.Sleep(120 * time.Millisecond)
					_, _ = io.WriteString(writer, "event: content_block_delta"+parts[1])
				}()
				c, _ := gin.CreateTestContext(httptest.NewRecorder())
				c.Request = httptest.NewRequest(http.MethodPost, "/v1/"+endpoint, nil)
				resp := &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: reader}
				account := nativeAnthropicTestAccount()
				account.Platform = platform
				start := time.Now()
				var result *OpenAIForwardResult
				var err error
				switch endpoint {
				case "messages":
					result, err = svc.handleNativeAnthropicStreamingResponse(context.Background(), resp, c, account, "model", "model", "model", nil, start)
				case "chat/completions":
					result, err = svc.handleCCStreamingFromNativeAnthropic(resp, c, "model", "model", "model", nil, start, true)
				case "responses":
					result, err = svc.handleResponsesStreamingFromNativeAnthropic(resp, c, "model", "model", "model", nil, start, apicompat.ResponsesClientToolMapping{})
				}
				require.NoError(t, err)
				require.NotNil(t, result)
				require.True(t, result.UsageComplete())
				require.NotNil(t, result.FirstTokenMs)
				require.GreaterOrEqual(t, *result.FirstTokenMs, 100)
				require.NotNil(t, result.LastTokenMs)
				require.GreaterOrEqual(t, *result.LastTokenMs, *result.FirstTokenMs)
				require.Equal(t, "text", result.FirstOutputKind)
				<-finished
			})
		}
	}
}

func TestNativeAnthropicAdaptersRequireActualUpstreamCompletion(t *testing.T) {
	for _, endpoint := range []string{"chat/completions", "responses"} {
		for _, mode := range []string{"complete", "truncated", "error_then_stop"} {
			t.Run(endpoint+"/"+mode, func(t *testing.T) {
				payload := miniAnthropicSSEStream()
				if mode == "truncated" {
					payload = strings.Split(payload, "event: message_stop")[0]
				} else if mode == "error_then_stop" {
					payload = strings.Replace(payload, "event: message_stop", "event: error\ndata: {\"type\":\"error\",\"error\":{\"type\":\"overloaded_error\",\"message\":\"synthetic\"}}\n\nevent: message_stop", 1)
				}
				svc := newNativeAnthropicHangTestService(5)
				recorder := httptest.NewRecorder()
				c, _ := gin.CreateTestContext(recorder)
				c.Request = httptest.NewRequest(http.MethodPost, "/v1/"+endpoint, nil)
				resp := &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(payload))}
				var result *OpenAIForwardResult
				var err error
				if endpoint == "responses" {
					result, err = svc.handleResponsesStreamingFromNativeAnthropic(resp, c, "model", "model", "model", nil, time.Now(), apicompat.ResponsesClientToolMapping{})
				} else {
					result, err = svc.handleCCStreamingFromNativeAnthropic(resp, c, "model", "model", "model", nil, time.Now(), true)
				}
				require.NoError(t, err)
				require.NotNil(t, result)
				require.Equal(t, mode == "complete", result.UsageComplete())
				require.NotNil(t, result.FirstTokenMs)
				require.NotNil(t, result.LastTokenMs)
				require.Contains(t, recorder.Body.String(), "Hello")
				require.Positive(t, result.Usage.OutputTokens)
			})
		}
	}
}
