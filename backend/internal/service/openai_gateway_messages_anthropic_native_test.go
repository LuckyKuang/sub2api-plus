//go:build unit

package service

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestForwardAsAnthropic_NativeAnthropicAccountUsesNativeEndpoint(t *testing.T) {
	gin.SetMode(gin.TestMode)

	body := []byte(`{"model":"glm-4-air","max_tokens":32,"messages":[{"role":"user","content":"hello"}],"stream":false}`)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")

	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header: http.Header{
			"Content-Type": []string{"application/json"},
			"X-Request-Id": []string{"rid_native_messages"},
		},
		Body: io.NopCloser(bytes.NewBufferString(`{"id":"msg_native","type":"message","role":"assistant","model":"glm-4-air","content":[{"type":"text","text":"ok"}],"stop_reason":"end_turn","usage":{"input_tokens":3,"output_tokens":2}}`)),
	}}
	svc := &OpenAIGatewayService{
		cfg:          rawChatCompletionsTestConfig(),
		httpUpstream: upstream,
	}
	account := &Account{
		ID:          7001,
		Name:        "native-anthropic",
		Platform:    PlatformZhipu,
		Type:        AccountTypeAPIKey,
		Concurrency: 1,
		Credentials: map[string]any{
			"api_key":      "sk-zhipu-test",
			"api_protocol": APIProtocolAnthropic,
			"base_url":     "http://upstream.example/api/anthropic",
			"model_mapping": map[string]any{
				"glm-4-air": "glm-4-air-2025",
			},
		},
	}

	result, err := svc.ForwardAsAnthropic(context.Background(), c, account, body, "", "")
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, "rid_native_messages", result.RequestID)
	require.Equal(t, "glm-4-air-2025", result.UpstreamModel)
	require.Equal(t, "/v1/messages", result.UpstreamEndpoint)
	require.Equal(t, 3, result.Usage.InputTokens)
	require.Equal(t, 2, result.Usage.OutputTokens)
	require.Equal(t, "http://upstream.example/api/anthropic/v1/messages", upstream.lastReq.URL.String())
	require.Equal(t, "glm-4-air-2025", gjson.GetBytes(upstream.lastBody, "model").String())
	require.Equal(t, "hello", gjson.GetBytes(upstream.lastBody, "messages.0.content").String())
	require.False(t, gjson.GetBytes(upstream.lastBody, "input").Exists())
	require.Equal(t, "msg_native", gjson.Get(rec.Body.String(), "id").String())
}

func TestForwardAsAnthropic_OpenAIOAuthConvergesFingerprintBeforePlusCacheAuthority(t *testing.T) {
	gin.SetMode(gin.TestMode)

	body := []byte(`{"model":"gpt-5.4","max_tokens":32,"messages":[{"role":"user","content":"hello"}],"stream":false}`)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Request.Header.Set("session-id", "messages-client-session")
	c.Request.Header.Set("x-codex-installation-id", "messages-client-installation")

	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusBadRequest,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(`{"error":{"type":"invalid_request_error","message":"stop after capture"}}`)),
	}}
	svc := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: upstream}
	account := &Account{
		ID:          7002,
		Name:        "openai-oauth-messages-fingerprint",
		Platform:    PlatformOpenAI,
		Type:        AccountTypeOAuth,
		Concurrency: 1,
		Credentials: map[string]any{
			"access_token":       "oauth-token",
			"chatgpt_account_id": "chatgpt-acc",
		},
		Extra: map[string]any{
			CodexFingerprintModeExtraKey: "session",
			"openai_device_id":           "messages-owner-installation",
		},
	}

	result, err := svc.ForwardAsAnthropic(context.Background(), c, account, body, "messages-cache", "gpt-5.4")
	require.Error(t, err)
	require.Nil(t, result)
	require.NotNil(t, upstream.lastReq)

	cacheIdentity := strings.TrimSpace(upstream.lastReq.Header.Get("session-id"))
	require.NotEmpty(t, cacheIdentity)
	require.Equal(t, cacheIdentity, upstream.lastReq.Header.Get("session_id"))
	require.Equal(t, "messages-owner-installation", upstream.lastReq.Header.Get("x-codex-installation-id"))
	require.Equal(t, "messages-owner-installation", gjson.GetBytes(upstream.lastBody, "client_metadata.x-codex-installation-id").String())
	require.Equal(t, resolveConvergedSessionID(account), gjson.GetBytes(upstream.lastBody, "client_metadata.session_id").String())
	require.Equal(t, upstream.lastReq.Header.Get("thread-id"), upstream.lastReq.Header.Get("x-client-request-id"))
	require.Empty(t, upstream.lastReq.Header.Get("conversation_id"))
}
