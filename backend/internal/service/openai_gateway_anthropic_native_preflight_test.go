//go:build unit

package service

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/config"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

const nativeStructuralPrelude = "event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_test\",\"type\":\"message\",\"role\":\"assistant\",\"model\":\"k3\",\"content\":[],\"usage\":{\"input_tokens\":10}}}\n\nevent: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"text\",\"text\":\"\"}}\n\n"

func nativeErrorSSE(kind string) string {
	return fmt.Sprintf("event: error\ndata: {\"type\":\"error\",\"error\":{\"type\":%q,\"message\":\"upstream failure\"}}\n\n", kind)
}

func nativeSSETestResponse(body string) *http.Response {
	return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(body))}
}

func forwardNativeSSETest(t *testing.T, endpoint string, stream bool, body string) (*OpenAIForwardResult, error, *httptest.ResponseRecorder) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, endpoint, nil)
	svc := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: &httpUpstreamRecorder{resp: nativeSSETestResponse(body)}}
	account := nativeAnthropicTestAccount()
	var result *OpenAIForwardResult
	var err error
	switch endpoint {
	case "/v1/responses":
		result, err = svc.Forward(context.Background(), c, account, []byte(fmt.Sprintf(`{"model":"k3","input":"hello","stream":%t}`, stream)))
	case "/v1/chat/completions":
		result, err = svc.ForwardAsChatCompletions(context.Background(), c, account, []byte(fmt.Sprintf(`{"model":"k3","messages":[{"role":"user","content":"hello"}],"stream":%t}`, stream)), "", "")
	case "/v1/messages":
		result, err = svc.ForwardAsAnthropic(context.Background(), c, account, []byte(fmt.Sprintf(`{"model":"k3","max_tokens":32,"messages":[{"role":"user","content":"hello"}],"stream":%t}`, stream)), "", "")
	}
	return result, err, recorder
}

func TestNativeAnthropicPreOutputErrorsAllowSafeAccountRecovery(t *testing.T) {
	for _, endpoint := range []string{"/v1/messages", "/v1/chat/completions", "/v1/responses"} {
		for _, kind := range []string{"authentication_error", "rate_limit_error"} {
			for _, stream := range []bool{true, false} {
				if endpoint == "/v1/messages" && !stream {
					continue
				}
				t.Run(fmt.Sprintf("%s/%s/stream=%t", endpoint, kind, stream), func(t *testing.T) {
					result, err, recorder := forwardNativeSSETest(t, endpoint, stream, nativeStructuralPrelude+nativeErrorSSE(kind))
					require.Nil(t, result)
					var failover *UpstreamFailoverError
					require.ErrorAs(t, err, &failover)
					require.True(t, failover.ShouldRetryNextAccount())
					require.Empty(t, recorder.Body.String(), "structural frames must not commit the client response before recovery")
				})
			}
		}
	}
}

func TestNativeAnthropicPostOutputErrorNeverReplaysOrBecomesBufferedSuccess(t *testing.T) {
	semantic := "event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"visible\"}}\n\n"
	for _, endpoint := range []string{"/v1/messages", "/v1/chat/completions", "/v1/responses"} {
		for _, stream := range []bool{true, false} {
			if endpoint == "/v1/messages" && !stream {
				continue
			}
			t.Run(fmt.Sprintf("%s/stream=%t", endpoint, stream), func(t *testing.T) {
				result, err, recorder := forwardNativeSSETest(t, endpoint, stream, nativeStructuralPrelude+semantic+nativeErrorSSE("authentication_error"))
				require.NotNil(t, result)
				require.True(t, result.UsageIncomplete)
				require.Equal(t, 10, result.Usage.InputTokens)
				var failover *UpstreamFailoverError
				require.Error(t, err)
				require.NotErrorAs(t, err, &failover)
				if stream {
					require.Contains(t, recorder.Body.String(), "visible")
				} else {
					require.Error(t, err)
					require.Equal(t, http.StatusBadGateway, recorder.Code)
				}
			})
		}
	}
}

func TestNativeAnthropicBufferedTruncationRetainsUsageAndFails(t *testing.T) {
	for _, endpoint := range []string{"/v1/chat/completions", "/v1/responses"} {
		t.Run(endpoint, func(t *testing.T) {
			body := strings.Split(miniAnthropicSSEStream(), "event: message_stop")[0]
			result, err, recorder := forwardNativeSSETest(t, endpoint, false, body)
			require.Error(t, err)
			var failover *UpstreamFailoverError
			require.NotErrorAs(t, err, &failover)
			require.NotNil(t, result)
			require.True(t, result.UsageIncomplete)
			require.Equal(t, 10, result.Usage.InputTokens)
			require.Equal(t, 5, result.Usage.OutputTokens)
			require.Equal(t, http.StatusBadGateway, recorder.Code)
		})
	}
}

func TestNativeAnthropicPreOutputSharedCapacityNeverRotatesAccounts(t *testing.T) {
	for _, endpoint := range []string{"/v1/messages", "/v1/chat/completions", "/v1/responses"} {
		t.Run(endpoint, func(t *testing.T) {
			result, err, recorder := forwardNativeSSETest(t, endpoint, true, nativeStructuralPrelude+nativeErrorSSE("overloaded_error"))
			require.Error(t, err)
			var failover *UpstreamFailoverError
			require.NotErrorAs(t, err, &failover, "provider-wide overload must not scan credentials")
			require.NotNil(t, result)
			require.True(t, result.UsageIncomplete)
			require.Contains(t, recorder.Body.String(), "error")
			require.NotContains(t, recorder.Body.String(), "response.completed")
			require.NotContains(t, recorder.Body.String(), `"finish_reason":"stop"`)
		})
	}
}

func TestNativeAnthropicPreflightPreservesUnknownAndSemanticFrames(t *testing.T) {
	svc := &OpenAIGatewayService{}
	for _, prefix := range []string{
		miniAnthropicSSEStream(),
		toolAnthropicSSEStream(),
		"event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"future_audio\",\"data\":\"payload\"}}\n\n",
		"event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"content\":[{\"type\":\"text\",\"text\":\"already generated\"}]}}\n\n",
		"event: content_block_start\ndata: {\"type\":\"content_block_start\",\"content_block\":{\"type\":\"text\",\"text\":\"\",\"future_output\":\"payload\"}}\n\n",
		"event: unknown\ndata: {\"type\":\"future_frame\",\"output\":\"payload\"}\n\n",
		"data: " + strings.Repeat("x", 70000) + "\n\n",
	} {
		body := prefix + nativeErrorSSE("authentication_error")
		resp := nativeSSETestResponse(body)
		require.NoError(t, svc.prepareNativeAnthropicResponse(context.Background(), resp))
		require.Equal(t, http.StatusOK, resp.StatusCode)
		got, err := io.ReadAll(resp.Body)
		require.NoError(t, err)
		require.Equal(t, body, string(got))
		require.NoError(t, resp.Body.Close())
	}
	resp := nativeSSETestResponse(strings.ReplaceAll(nativeStructuralPrelude+nativeErrorSSE("authentication_error"), "\n", "\r\n"))
	require.NoError(t, svc.prepareNativeAnthropicResponse(context.Background(), resp))
	require.Equal(t, http.StatusUnauthorized, resp.StatusCode)
}

func TestNativeAnthropicPreflightCancellationClosesBlockedBody(t *testing.T) {
	pr, pw := io.Pipe()
	defer pw.Close()
	resp := nativeSSETestResponse("")
	resp.Body = pr
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- (&OpenAIGatewayService{}).prepareNativeAnthropicResponse(ctx, resp) }()
	cancel()
	select {
	case err := <-done:
		require.ErrorIs(t, err, context.Canceled)
	case <-time.After(3 * time.Second):
		t.Fatal("cancellation must close the blocked upstream reader")
	}
}

func TestNativeAnthropicPreflightDeadlineReleasesKeepaliveAndPreservesPendingRead(t *testing.T) {
	pr, pw := io.Pipe()
	defer pw.Close()
	resp := nativeSSETestResponse("")
	resp.Body = pr
	svc := &OpenAIGatewayService{cfg: &config.Config{Gateway: config.GatewayConfig{StreamKeepaliveInterval: 1}}}
	done := make(chan error, 1)
	go func() { done <- svc.prepareNativeAnthropicResponse(context.Background(), resp) }()
	prelude := "event: ping\ndata: {\"type\":\"ping\"}\n\n"
	_, err := io.WriteString(pw, prelude)
	require.NoError(t, err)
	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(3 * time.Second):
		t.Fatal("preflight must release healthy structural frames before keepalive deadline")
	}
	suffix := nativeErrorSSE("authentication_error")
	go func() { _, _ = io.WriteString(pw, suffix); _ = pw.Close() }()
	got, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.Equal(t, prelude+suffix, string(got))
	require.Equal(t, http.StatusOK, resp.StatusCode, "after the deadline a late error must not authorize replay")
	require.NoError(t, resp.Body.Close())
}
