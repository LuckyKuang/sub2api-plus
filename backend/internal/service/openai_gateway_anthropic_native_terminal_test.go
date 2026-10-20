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

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type nativeTerminalForwardOutcome struct {
	result *OpenAIForwardResult
	err    error
}

func forwardNativePipeTest(t *testing.T, endpoint string, stream bool, interval int, payload string) (nativeTerminalForwardOutcome, *httptest.ResponseRecorder) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	reader, writer := io.Pipe()
	t.Cleanup(func() { _ = writer.Close(); _ = reader.Close() })
	resp := nativeSSETestResponse("")
	resp.Body = reader
	cfg := rawChatCompletionsTestConfig()
	cfg.Gateway.StreamDataIntervalTimeout = interval
	svc := &OpenAIGatewayService{cfg: cfg, httpUpstream: &httpUpstreamRecorder{resp: resp}}
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, endpoint, nil)
	done := make(chan nativeTerminalForwardOutcome, 1)
	go func() {
		var result *OpenAIForwardResult
		var err error
		if endpoint == "/v1/responses" {
			result, err = svc.Forward(context.Background(), c, nativeAnthropicTestAccount(), []byte(fmt.Sprintf(`{"model":"k3","input":"hello","stream":%t}`, stream)))
		} else if endpoint == "/v1/messages" {
			result, err = svc.ForwardAsAnthropic(context.Background(), c, nativeAnthropicTestAccount(), []byte(fmt.Sprintf(`{"model":"k3","max_tokens":32,"messages":[{"role":"user","content":"hello"}],"stream":%t}`, stream)), "", "")
		} else {
			result, err = svc.ForwardAsChatCompletions(context.Background(), c, nativeAnthropicTestAccount(), []byte(fmt.Sprintf(`{"model":"k3","messages":[{"role":"user","content":"hello"}],"stream":%t}`, stream)), "", "")
		}
		done <- nativeTerminalForwardOutcome{result: result, err: err}
	}()
	go func() { _, _ = io.WriteString(writer, payload) }()
	select {
	case outcome := <-done:
		return outcome, recorder
	case <-time.After(4 * time.Second):
		_ = writer.Close()
		_ = reader.Close()
		t.Fatal("native response must complete without waiting for upstream connection close")
		return nativeTerminalForwardOutcome{}, recorder
	}
}

func TestNativeAnthropicTerminalCompletesWithoutUpstreamEOF(t *testing.T) {
	for _, endpoint := range []string{"/v1/chat/completions", "/v1/responses"} {
		for _, stream := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/stream=%t", endpoint, stream), func(t *testing.T) {
				outcome, recorder := forwardNativePipeTest(t, endpoint, stream, 10, miniAnthropicSSEStream())
				require.NoError(t, outcome.err)
				require.NotNil(t, outcome.result)
				require.False(t, outcome.result.UsageIncomplete)
				require.Equal(t, 10, outcome.result.Usage.InputTokens)
				require.Equal(t, 5, outcome.result.Usage.OutputTokens)
				require.Equal(t, http.StatusOK, recorder.Code)
				require.Contains(t, recorder.Body.String(), "Hello")
				if stream {
					if endpoint == "/v1/responses" {
						require.Contains(t, recorder.Body.String(), "response.completed")
					} else {
						require.Contains(t, recorder.Body.String(), "[DONE]")
					}
				}
			})
		}
	}
}

func TestNativeAnthropicBufferedIdleRetainsObservedUsage(t *testing.T) {
	for _, endpoint := range []string{"/v1/chat/completions", "/v1/responses"} {
		t.Run(endpoint, func(t *testing.T) {
			partial := strings.Split(miniAnthropicSSEStream(), "event: message_stop")[0]
			outcome, recorder := forwardNativePipeTest(t, endpoint, false, 1, partial)
			require.Error(t, outcome.err)
			var failover *UpstreamFailoverError
			require.NotErrorAs(t, outcome.err, &failover)
			require.NotNil(t, outcome.result)
			require.True(t, outcome.result.UsageIncomplete)
			require.Equal(t, 10, outcome.result.Usage.InputTokens)
			require.Equal(t, 5, outcome.result.Usage.OutputTokens)
			require.Equal(t, http.StatusBadGateway, recorder.Code)
		})
	}
}

func TestNativeAnthropicErrorThenStopRemainsFailed(t *testing.T) {
	partial := strings.Split(miniAnthropicSSEStream(), "event: message_stop")[0]
	body := partial + nativeErrorSSE("authentication_error") + "event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"
	for _, endpoint := range []string{"/v1/chat/completions", "/v1/responses"} {
		for _, stream := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/stream=%t", endpoint, stream), func(t *testing.T) {
				result, err, recorder := forwardNativeSSETest(t, endpoint, stream, body)
				require.Error(t, err)
				var failover *UpstreamFailoverError
				require.NotErrorAs(t, err, &failover)
				require.NotNil(t, result)
				require.True(t, result.UsageIncomplete)
				require.Equal(t, 10, result.Usage.InputTokens)
				require.Equal(t, 5, result.Usage.OutputTokens)
				require.NotContains(t, recorder.Body.String(), "response.completed")
				require.NotContains(t, recorder.Body.String(), "[DONE]")
			})
		}
	}
}

func TestNativeAnthropicMessagesTerminalFrameCompletesWithoutUpstreamEOF(t *testing.T) {
	outcome, recorder := forwardNativePipeTest(t, "/v1/messages", true, 10, miniAnthropicSSEStream())
	require.NoError(t, outcome.err)
	require.NotNil(t, outcome.result)
	require.False(t, outcome.result.UsageIncomplete)
	require.Equal(t, 10, outcome.result.Usage.InputTokens)
	require.Equal(t, 5, outcome.result.Usage.OutputTokens)
	require.Equal(t, http.StatusOK, recorder.Code)
	require.Equal(t, miniAnthropicSSEStream(), recorder.Body.String(), "the terminal data and blank event boundary must reach the client before completion")
}

func TestNativeAnthropicMessagesErrorThenStopPreservesFailureAndTerminalFrame(t *testing.T) {
	partial := strings.Split(miniAnthropicSSEStream(), "event: message_stop")[0]
	body := partial + nativeErrorSSE("authentication_error") + "event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"
	result, err, recorder := forwardNativeSSETest(t, "/v1/messages", true, body)
	require.Error(t, err)
	var failover *UpstreamFailoverError
	require.NotErrorAs(t, err, &failover)
	require.NotNil(t, result)
	require.True(t, result.UsageIncomplete)
	require.Equal(t, 10, result.Usage.InputTokens)
	require.Equal(t, 5, result.Usage.OutputTokens)
	require.Equal(t, body, recorder.Body.String())
}

func TestNativeAnthropicMessagesTerminalNameWithoutPayloadRemainsIncomplete(t *testing.T) {
	partial := strings.Split(miniAnthropicSSEStream(), "event: message_stop")[0]
	result, err, _ := forwardNativeSSETest(t, "/v1/messages", true, partial+"event: message_stop\n")
	require.Error(t, err)
	require.NotNil(t, result)
	require.True(t, result.UsageIncomplete)
	require.Equal(t, 10, result.Usage.InputTokens)
	require.Equal(t, 5, result.Usage.OutputTokens)
}
