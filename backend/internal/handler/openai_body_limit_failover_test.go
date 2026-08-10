package handler

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/LuckyKuang/sub2api-plus/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestOpenAIBodyLimitFailoverExhausted_ReturnsRedactedJSON413(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(nil))

	(&OpenAIGatewayHandler{}).handleFailoverExhausted(c, bodyLimitFailoverTestError(), false)

	require.Equal(t, http.StatusRequestEntityTooLarge, rec.Code)
	var envelope map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &envelope))
	errBody, ok := envelope["error"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "invalid_request_error", errBody["type"])
	require.Equal(t, "Request payload is too large", errBody["message"])
	require.NotContains(t, rec.Body.String(), "must-not-leak")
}

func TestOpenAIBodyLimitFailoverExhausted_ReturnsRedactedResponsesSSE(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(nil))

	(&OpenAIGatewayHandler{}).handleFailoverExhausted(c, bodyLimitFailoverTestError(), true)

	body := rec.Body.String()
	require.True(t, strings.HasPrefix(body, "event: response.failed\n"))
	require.Contains(t, body, `"code":"invalid_request"`)
	require.Contains(t, body, `"message":"Request payload is too large"`)
	require.NotContains(t, body, "must-not-leak")
}

func TestOpenAIProxyRetryBufferLimitFailoverExhausted_ReturnsStableJSON413(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(nil))

	(&OpenAIGatewayHandler{}).handleFailoverExhausted(c, &service.UpstreamFailoverError{
		StatusCode:        http.StatusInsufficientStorage,
		ResponseBody:      []byte(`{"error":{"message":"exceeded request buffer limit while retrying upstream secret=must-not-leak"}}`),
		Scope:             service.GatewayFailureScopeRequest,
		Reason:            service.GatewayFailureReason("openai_proxy_retry_buffer_limit"),
		NextAccountAction: service.NextAccountStop,
		ClientStatusCode:  http.StatusRequestEntityTooLarge,
		ClientMessage:     service.OpenAIProxyRetryBufferLimitClientMessage,
	}, false)

	require.Equal(t, http.StatusRequestEntityTooLarge, rec.Code)
	require.Contains(t, rec.Body.String(), service.OpenAIProxyRetryBufferLimitClientMessage)
	require.NotContains(t, rec.Body.String(), "must-not-leak")
}

func bodyLimitFailoverTestError() *service.UpstreamFailoverError {
	return &service.UpstreamFailoverError{
		StatusCode:        http.StatusRequestEntityTooLarge,
		ResponseBody:      []byte(`{"error":{"message":"proxy limit secret=must-not-leak"}}`),
		Scope:             service.GatewayFailureScopeAccount,
		Reason:            service.GatewayFailureReason("openai_request_body_too_large"),
		NextAccountAction: service.NextAccountRetry,
		ClientStatusCode:  http.StatusRequestEntityTooLarge,
		ClientMessage:     "Request payload is too large",
	}
}
