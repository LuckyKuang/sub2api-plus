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

	"github.com/LuckyKuang/sub2api-plus/internal/config"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

// Approved audit acceptance: an explicit encrypted-history rejection carried
// by HTTP 200/SSE is recoverable once before output, with continuation and tool
// history intact. A committed answer/tool action must never be replayed.
const auditEncryptedFailure = "event: response.failed\ndata: {\"type\":\"response.failed\",\"sequence_number\":0,\"response\":{\"id\":\"resp_rejected\",\"status\":\"failed\",\"error\":{\"code\":\"invalid_encrypted_content\",\"type\":\"invalid_request_error\",\"message\":\"Encrypted content could not be decrypted or parsed.\"},\"output\":[]}}\n\n"
const auditEncryptedSuccess = "event: response.created\ndata: {\"type\":\"response.created\",\"sequence_number\":0,\"response\":{\"id\":\"resp_recovered\"}}\n\nevent: response.output_text.delta\ndata: {\"type\":\"response.output_text.delta\",\"sequence_number\":1,\"delta\":\"ok\"}\n\nevent: response.completed\ndata: {\"type\":\"response.completed\",\"sequence_number\":2,\"response\":{\"id\":\"resp_recovered\",\"status\":\"completed\",\"usage\":{\"input_tokens\":3,\"output_tokens\":2}}}\n\n"

func auditSSEResponse(stream string) *http.Response {
	return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(stream))}
}

func TestAuditSSEEncryptedRecovery(t *testing.T) {
	for _, passthrough := range []bool{false, true} {
		for _, tc := range []struct {
			name, prefix, second string
			calls                int
			success              bool
		}{
			{"recovers", "", auditEncryptedSuccess, 2, true},
			{"preamble_is_not_output", "event: response.created\ndata: {\"type\":\"response.created\",\"response\":{\"id\":\"resp_rejected\"}}\n\n", auditEncryptedSuccess, 2, true},
			{"stops_after_one_retry", "", auditEncryptedFailure, 2, false},
			{"text_committed", "data: {\"type\":\"response.output_text.delta\",\"delta\":\"already delivered\"}\n\n", auditEncryptedSuccess, 1, false},
			{"tool_committed", "data: {\"type\":\"response.function_call_arguments.delta\",\"delta\":\"{\\\"action\\\":\\\"write\\\"}\"}\n\n", auditEncryptedSuccess, 1, false},
		} {
			t.Run(fmt.Sprintf("%s/passthrough=%v", tc.name, passthrough), func(t *testing.T) {
				cfg := &config.Config{}
				cfg.Security.URLAllowlist.Enabled = false
				cfg.Security.URLAllowlist.AllowInsecureHTTP = true
				upstream := &httpUpstreamSequenceRecorder{responses: []*http.Response{auditSSEResponse(tc.prefix + auditEncryptedFailure), auditSSEResponse(tc.second)}}
				svc := &OpenAIGatewayService{cfg: cfg, httpUpstream: upstream}
				account := &Account{ID: 66, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Credentials: map[string]any{"base_url": "http://example.test", "api_key": "test-key"}, Extra: map[string]any{"openai_passthrough": passthrough}}
				w := httptest.NewRecorder()
				c, _ := gin.CreateTestContext(w)
				c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
				body := []byte(`{"model":"gpt-5.5","stream":true,"previous_response_id":"resp_previous","input":[{"type":"reasoning","encrypted_content":"rejected-cipher","summary":[{"type":"summary_text","text":"keep summary"}]},{"type":"function_call","call_id":"call_keep","name":"read_file","arguments":"{}"},{"type":"function_call_output","call_id":"call_keep","output":"keep result"},{"role":"user","content":"continue"}]}`)
				result, err := svc.Forward(context.Background(), c, account, body)
				require.Equal(t, tc.calls, upstream.callCount)
				if tc.success {
					require.NoError(t, err)
					require.NotNil(t, result)
					require.Equal(t, 2, result.Usage.OutputTokens)
					require.Equal(t, 3, result.Usage.InputTokens, "only the winning response contributes final usage")
					require.False(t, IsResponseCommitted(c), "recovered rejection must not poison the final response state")
					_, marked := GetOpsStreamError(c)
					require.False(t, marked, "a recovered attempt is not a final failed request")
					events, _ := c.Get(OpsUpstreamErrorsKey)
					attempts := events.([]*OpsUpstreamErrorEvent)
					require.Len(t, attempts, 1)
					require.Equal(t, "retry", attempts[0].Kind)
					require.Equal(t, "invalid_encrypted_content", attempts[0].Reason)
					require.Equal(t, int64(66), attempts[0].AccountID)
					require.NotNil(t, attempts[0].SemanticOutputCommitted)
					require.False(t, *attempts[0].SemanticOutputCommitted)
					require.Nil(t, attempts[0].TimeToFirstTokenMs)
					require.NotContains(t, w.Body.String(), "resp_rejected")
					require.NotContains(t, w.Body.String(), "response.failed")
					require.Equal(t, 1, strings.Count(w.Body.String(), "event: response.completed"))
				} else {
					require.Error(t, err)
					require.Equal(t, 1, strings.Count(w.Body.String(), "event: response.failed"))
				}
				if tc.calls == 2 {
					retry := upstream.bodies[1]
					require.Equal(t, "resp_previous", gjson.GetBytes(retry, "previous_response_id").String())
					require.False(t, gjson.GetBytes(retry, "input.0.encrypted_content").Exists())
					require.Equal(t, "keep summary", gjson.GetBytes(retry, "input.0.summary.0.text").String())
					require.Equal(t, "call_keep", gjson.GetBytes(retry, "input.1.call_id").String())
					require.Equal(t, "keep result", gjson.GetBytes(retry, "input.2.output").String())
					require.Equal(t, "continue", gjson.GetBytes(retry, "input.3.content").String())
					require.NotEmpty(t, upstream.reqs[0].Header.Get("User-Agent"))
					require.Equal(t, upstream.reqs[0].Header.Get("User-Agent"), upstream.reqs[1].Header.Get("User-Agent"), "same credential owner keeps its identity snapshot")
					require.Equal(t, "Bearer test-key", upstream.reqs[1].Header.Get("Authorization"))
				}
			})
		}
	}
}

func TestAuditEncryptedRecoveryRequiresExplicitCodeAndUsableInput(t *testing.T) {
	for _, tc := range []struct {
		name, body, failure string
		firstHTTP           bool
	}{
		{"reasoning_id_only", `{"model":"gpt-5.5","stream":true,"input":[{"type":"reasoning","id":"rs_rejected","encrypted_content":"cipher","summary":[]}]}`, auditEncryptedFailure, false},
		{"opaque_only", `{"model":"gpt-5.5","stream":true,"input":[{"type":"compaction","encrypted_content":"cipher"}]}`, auditEncryptedFailure, false},
		{"null_reasoning_without_cipher", `{"model":"gpt-5.5","stream":true,"input":[{"type":"reasoning","content":null},{"role":"user","content":"hello"}]}`, auditEncryptedFailure, false},
		{"no_cipher", `{"model":"gpt-5.5","stream":true,"input":"hello"}`, auditEncryptedFailure, false},
		{"message_only", `{"model":"gpt-5.5","stream":true,"input":[{"type":"reasoning","encrypted_content":"cipher"},{"role":"user","content":"hello"}]}`, strings.ReplaceAll(auditEncryptedFailure, "invalid_encrypted_content", "invalid_request"), false},
		{"shared_http_sse_budget", `{"model":"gpt-5.5","stream":true,"input":[{"type":"reasoning","encrypted_content":"cipher"},{"role":"user","content":"hello"}]}`, auditEncryptedFailure, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := &config.Config{}
			cfg.Security.URLAllowlist.AllowInsecureHTTP = true
			first := auditSSEResponse(tc.failure)
			if tc.firstHTTP {
				first = &http.Response{StatusCode: 400, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{"error":{"type":"invalid_request_error","code":"invalid_encrypted_content"}}`))}
			}
			upstream := &httpUpstreamSequenceRecorder{responses: []*http.Response{first, auditSSEResponse(tc.failure), auditSSEResponse(auditEncryptedSuccess)}}
			svc := &OpenAIGatewayService{cfg: cfg, httpUpstream: upstream}
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest("POST", "/v1/responses", nil)
			account := &Account{ID: 66, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Credentials: map[string]any{"base_url": "http://example.test", "api_key": "test-key"}}
			_, err := svc.Forward(context.Background(), c, account, []byte(tc.body))
			require.Error(t, err)
			expected := 1
			if tc.firstHTTP {
				expected = 2
			}
			require.Equal(t, expected, upstream.callCount)
		})
	}
}

// Responses error events use a top-level code; stream=false upstreams may
// still reply in SSE. Both are provider wire contracts, not implementation shapes.
func TestAuditEncryptedCodeOnBareErrorAndNonStreamingSSE(t *testing.T) {
	for _, stream := range []bool{false, true} {
		for _, passthrough := range []bool{false, true} {
			t.Run(fmt.Sprintf("stream=%v/passthrough=%v", stream, passthrough), func(t *testing.T) {
				failure := "event: error\ndata: {\"type\":\"error\",\"code\":\"invalid_encrypted_content\",\"message\":\"Encrypted history rejected\",\"param\":null}\n\n"
				cfg := &config.Config{}
				cfg.Security.URLAllowlist.AllowInsecureHTTP = true
				upstream := &httpUpstreamSequenceRecorder{responses: []*http.Response{auditSSEResponse(failure), auditSSEResponse(auditEncryptedSuccess)}}
				svc := &OpenAIGatewayService{cfg: cfg, httpUpstream: upstream}
				c, _ := gin.CreateTestContext(httptest.NewRecorder())
				c.Request = httptest.NewRequest("POST", "/v1/responses", nil)
				account := &Account{ID: 66, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Credentials: map[string]any{"base_url": "http://example.test", "api_key": "test-key"}, Extra: map[string]any{"openai_passthrough": passthrough}}
				body := fmt.Sprintf(`{"model":"gpt-5.5","stream":%v,"input":[{"type":"reasoning","encrypted_content":"cipher"},{"role":"user","content":"continue"}]}`, stream)
				result, err := svc.Forward(context.Background(), c, account, []byte(body))
				require.Equal(t, 2, upstream.callCount)
				require.NoError(t, err)
				require.Equal(t, 2, result.Usage.OutputTokens)
			})
		}
	}
}
