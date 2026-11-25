//go:build unit

package service

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/pkg/ctxkey"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func saveErrorMessages(t *testing.T, s *SettingService, enabled bool) {
	t.Helper()
	require.NoError(t, s.persistSystemSettingsWithCodexSafetyBuffering(t.Context(), &SystemSettings{}, map[string]string{SettingKeyOpenAIErrorMessagesNormalized: strconvBool(enabled)}))
}

// Expectations come from the three requested error codes and protocol shapes;
// compare the entire document so a lost code, usage or opaque field fails.
func TestOpenAIErrorMessagesProtocolContract(t *testing.T) {
	for _, tc := range []struct{ code, message, publicCode, errorType string }{
		{"upstream_response_interrupted", "本次响应已中断。请先检查已收到的内容，再决定是否继续。", "upstream_response_interrupted", "server_error"},
		{"upstream_unavailable", "服务暂时不可用，请稍后再试。", "upstream_unavailable", "service_unavailable_error"},
		{"session_account_lost", "当前会话可能无法恢复，请新开会话继续。", "session_account_lost", "server_error"},
		{"insufficient_quota", "当前服务额度不可用，请联系管理员。", "service_capacity_unavailable", "service_unavailable_error"},
	} {
		for _, envelope := range []struct{ input, want string }{
			{`{"error":{"code":"%s","message":"original","type":"upstream_error","param":"input","opaque":7}}`, `{"error":{"code":"%s","message":"%s","type":"%s","param":"input"}}`},
			{`{"type":"error","sequence_number":9,"error":{"code":"%s","message":"original","type":"upstream_error"}}`, `{"type":"error","sequence_number":9,"error":{"code":"%s","message":"%s","type":"%s"}}`},
			{`{"type":"error","code":"%s","message":"original","sequence_number":9}`, `{"type":"error","code":"%s","message":"%s","sequence_number":9}`},
			{`{"type":"response.failed","response":{"id":"resp_1","status":"failed","error":{"code":"%s","message":"original","type":"upstream_error"},"usage":{"input_tokens":12,"output_tokens":3}},"opaque":true}`, `{"type":"response.failed","response":{"id":"resp_1","status":"failed","error":{"code":"%s","message":"%s","type":"%s"},"usage":{"input_tokens":12,"output_tokens":3}}}`},
			{`{"type":"response.failed","error":{"code":"%s","message":"original","type":"upstream_error"},"opaque":true}`, `{"type":"response.failed","error":{"code":"%s","message":"%s","type":"%s"}}`},
		} {
			input := fmt.Sprintf(envelope.input, tc.code)
			args := []any{tc.publicCode, tc.message, tc.errorType}
			want := fmt.Sprintf(envelope.want, args[:strings.Count(envelope.want, "%s")]...)
			require.JSONEq(t, want, string(normalizeOpenAIErrorMessage([]byte(input), openAIErrorMessageSource{})))
		}
	}
	for _, input := range []string{
		`{"type":"future","error":{"code":"session_account_lost","message":"original"}}`,
		`{"type":"response.completed","response":{"error":{"code":"session_account_lost","message":"original"}}}`,
		`[{"error":{"code":"upstream_unavailable","message":"original"}}]`,
		`{"error":`, `[DONE]`,
	} {
		require.Equal(t, input, string(normalizeOpenAIErrorMessage([]byte(input), openAIErrorMessageSource{})))
	}
	for _, input := range []string{
		`{"error":{"code":"upstream_stream_read_error","message":"original"}}`,
		`{"error":{"code":"UPSTREAM_UNAVAILABLE","message":"original"}}`,
		`{"error":{"code":"upstream_unavailable_extra","message":"original"}}`,
		`{"error":{"code":"upstream_unavailable","message":null}}`,
		`{"error":{"code":"upstream_unavailable"}}`,
		`{"error":{"code":42,"message":"original"}}`,
	} {
		got := string(normalizeOpenAIErrorMessage([]byte(input), openAIErrorMessageSource{}))
		require.NotContains(t, got, "original")
		require.Contains(t, got, "message")
	}
}

func TestOpenAIErrorMessagesLiveHTTPBoundary(t *testing.T) {
	for _, protocol := range []string{"json", "sse", "sse-eof"} {
		for _, platform := range []string{PlatformOpenAI, PlatformAnthropic, PlatformGrok, ""} {
			t.Run(protocol+"/"+platform, func(t *testing.T) {
				svc, _ := safetyBufferingTestService(t)
				rec := httptest.NewRecorder()
				c, _ := gin.CreateTestContext(rec)
				c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
				OpenAIErrorMessageOutput(svc.settingService, func(c *gin.Context) {
					c.Request = c.Request.WithContext(context.WithValue(c.Request.Context(), ctxkey.Platform, platform))
					c.Set(string(ctxkey.Platform), platform)
					payload := `{"error":{"code":"upstream_unavailable","message":"original","type":"upstream_error"}}`
					contentType := "application/json"
					if protocol != "json" {
						contentType = "text/event-stream"
						payload = "id: 42\nevent: error\ndata: " + payload
						if protocol == "sse" {
							payload += "\n\n"
						}
					}
					c.Header("Content-Type", contentType)
					c.Status(http.StatusBadGateway)
					middle := len(payload) / 2
					_, err := c.Writer.WriteString(payload[:middle])
					require.NoError(t, err)
					_, err = c.Writer.WriteString(payload[middle:])
					require.NoError(t, err)
				})(c)
				require.Equal(t, http.StatusBadGateway, rec.Code)
				if platform == PlatformOpenAI {
					require.Contains(t, rec.Body.String(), "服务暂时不可用，请稍后再试。")
					require.NotContains(t, rec.Body.String(), "original")
					require.Contains(t, rec.Body.String(), `"code":"upstream_unavailable"`)
				} else {
					require.Contains(t, rec.Body.String(), "original")
				}
			})
		}
	}
}

func TestOpenAIErrorMessagesSettingsPersistenceAndReplicas(t *testing.T) {
	svc, repo := safetyBufferingTestService(t)
	s := svc.settingService
	require.False(t, s.OpenAIErrorMessagesNormalized(t.Context()))
	require.False(t, s.CodexSafetyBufferingSuppressed(t.Context()))
	saveErrorMessages(t, s, true)
	require.True(t, NewSettingService(repo, nil).OpenAIErrorMessagesNormalized(t.Context()))
	require.False(t, s.CodexSafetyBufferingSuppressed(t.Context()))
	saveSafetyBuffering(t, s, true)
	require.True(t, s.OpenAIErrorMessagesNormalized(t.Context()))
	repo.mu.Lock()
	repo.writeErr = errors.New("unavailable")
	repo.mu.Unlock()
	require.Error(t, s.persistSystemSettingsWithCodexSafetyBuffering(t.Context(), &SystemSettings{}, map[string]string{SettingKeyOpenAIErrorMessagesNormalized: "false"}))
	require.True(t, s.OpenAIErrorMessagesNormalized(t.Context()))
	repo.mu.Lock()
	repo.writeErr = nil
	repo.readErr = errors.New("unavailable")
	repo.mu.Unlock()
	s.refreshCodexSafetyBuffering(t.Context())
	require.True(t, s.OpenAIErrorMessagesNormalized(t.Context()))
	repo.mu.Lock()
	repo.readErr = nil
	repo.values[SettingKeyOpenAIErrorMessagesNormalized] = "invalid"
	repo.mu.Unlock()
	s.refreshCodexSafetyBuffering(t.Context())
	require.True(t, s.OpenAIErrorMessagesNormalized(t.Context()))
	saveErrorMessages(t, s, true)
	bus := &safetyBufferingInvalidationBus{}
	other := NewSettingService(repo, nil)
	s.StartCodexSafetyBufferingRuntime(t.Context(), bus)
	other.StartCodexSafetyBufferingRuntime(t.Context(), bus)
	t.Cleanup(s.StopCodexSafetyBufferingRuntime)
	t.Cleanup(other.StopCodexSafetyBufferingRuntime)
	require.Eventually(t, func() bool { bus.mu.Lock(); defer bus.mu.Unlock(); return len(bus.listeners) == 2 }, time.Second, time.Millisecond)
	saveErrorMessages(t, s, false)
	require.False(t, other.OpenAIErrorMessagesNormalized(t.Context()), "broadcast updates an already running replica")
	bus.mu.Lock()
	bus.drop = true
	bus.mu.Unlock()
	saveErrorMessages(t, s, true)
	require.Eventually(t, func() bool { return other.OpenAIErrorMessagesNormalized(t.Context()) }, 3*time.Second, 10*time.Millisecond)
	require.True(t, other.CodexSafetyBufferingSuppressed(t.Context()))
}

func TestOpenAIErrorMessagesLargeEventAndContentBoundaries(t *testing.T) {
	opaque := strings.Repeat("x", 5*1024*1024)
	for _, contentType := range []string{"application/json", "text/event-stream"} {
		t.Run(contentType, func(t *testing.T) {
			svc, _ := safetyBufferingTestService(t)
			rec := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(rec)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
			c.Set(string(ctxkey.Platform), PlatformOpenAI)
			payload := `{"error":{"code":"upstream_unavailable","type":"upstream_error","message":"original diagnostic"},"opaque":"` + opaque + `","item":{"error":{"code":"session_account_lost","message":"keep tool content"}}}`
			wire := payload
			if contentType == "text/event-stream" {
				wire = "id: 42\nevent: error\ndata: " + payload + "\n\n"
			}
			OpenAIErrorMessageOutput(svc.settingService, func(c *gin.Context) {
				c.Header("Content-Type", contentType)
				c.Status(http.StatusBadGateway)
				for start := 0; start < len(wire); start += 32768 {
					_, err := c.Writer.WriteString(wire[start:min(start+32768, len(wire))])
					require.NoError(t, err)
				}
			})(c)
			body := strings.TrimSpace(strings.TrimPrefix(rec.Body.String(), "id: 42\nevent: error\ndata: "))
			require.Equal(t, "服务暂时不可用，请稍后再试。", gjson.Get(body, "error.message").String())
			require.NotContains(t, body, opaque[:1024], "unapproved opaque extension must be removed")
			require.False(t, gjson.Get(body, "item").Exists(), "failure envelope keeps only approved fields")
			require.Equal(t, http.StatusBadGateway, rec.Code)
		})
	}
}

func TestOpenAIErrorMessagesEOFFailureRetainsUsage(t *testing.T) {
	for _, passthrough := range []bool{false, true} {
		for _, enabled := range []bool{false, true} {
			t.Run(fmt.Sprintf("passthrough=%t/enabled=%t", passthrough, enabled), func(t *testing.T) {
				svc, _ := safetyBufferingTestService(t)
				saveErrorMessages(t, svc.settingService, enabled)
				rec := &safetyBufferingTerminalWriteFailureRecorder{httptest.NewRecorder()}
				c, _ := gin.CreateTestContext(rec)
				c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
				c.Set(string(ctxkey.Platform), PlatformOpenAI)
				OpenAIErrorMessageOutput(svc.settingService, func(c *gin.Context) {
					wire := "data: {\"type\":\"response.output_text.delta\",\"delta\":\"answer\"}\n\ndata: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_eof\",\"status\":\"completed\",\"usage\":{\"input_tokens\":12,\"output_tokens\":3}}}\n"
					resp := &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(wire))}
					owner := &Account{ID: 7, Platform: PlatformOpenAI, Type: AccountTypeAPIKey}
					if passthrough {
						result, err := svc.handleStreamingResponsePassthrough(t.Context(), resp, c, owner, time.Now(), "fixture", "fixture")
						require.ErrorContains(t, err, "client disconnected")
						require.True(t, result.clientDisconnect)
						require.Equal(t, 12, result.usage.InputTokens)
						require.Equal(t, 3, result.usage.OutputTokens)
					} else {
						result, err := svc.handleStreamingResponse(t.Context(), resp, c, owner, time.Now(), "fixture", "fixture")
						require.ErrorContains(t, err, "client disconnected")
						require.True(t, result.clientDisconnect)
						require.Equal(t, 12, result.usage.InputTokens)
						require.Equal(t, 3, result.usage.OutputTokens)
					}
				})(c)
				require.Contains(t, rec.Body.String(), `"delta":"answer"`)
				require.NotContains(t, rec.Body.String(), `"type":"response.completed"`)
			})
		}
	}
}

func TestOpenAIErrorMessagesGatewayAccountAndCLIMatrix(t *testing.T) {
	for _, kind := range []string{AccountTypeOAuth, AccountTypeAPIKey} {
		for _, cli := range []bool{false, true} {
			t.Run(kind+"/cli="+strconvBool(cli), func(t *testing.T) {
				var sends atomic.Int32
				upstream := &codexModelsHTTPUpstreamStub{do: func(req *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
					sends.Add(1)
					return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(`data: {"type":"response.output_text.delta","delta":"partial answer"}

data: {"type":"response.failed","response":{"id":"resp_error","status":"failed","error":{"code":"upstream_unavailable","message":"original diagnostic"},"usage":{"input_tokens":12,"output_tokens":3}}}

`))}, nil
				}}
				svc, owner, bridges := safetyBufferingGatewayFixture(t, kind, cli, nil, upstream)
				saveSafetyBuffering(t, svc.settingService, false)
				rec := httptest.NewRecorder()
				c, _ := gin.CreateTestContext(rec)
				body := `{"model":"gpt-5.1","input":"hello","stream":true}`
				c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(body))
				c.Request = c.Request.WithContext(context.WithValue(c.Request.Context(), ctxkey.Platform, PlatformOpenAI))
				OpenAIErrorMessageOutput(svc.settingService, func(c *gin.Context) {
					result, err := svc.Forward(t.Context(), c, owner, []byte(body))
					require.Error(t, err)
					require.Contains(t, err.Error(), "original diagnostic", "internal error must retain the upstream diagnostic")
					require.NotNil(t, result)
					require.Equal(t, 12, result.Usage.InputTokens)
					require.Equal(t, 3, result.Usage.OutputTokens)
				})(c)
				require.Contains(t, rec.Body.String(), "partial answer")
				require.Contains(t, rec.Body.String(), `"status":"failed"`)
				require.Contains(t, rec.Body.String(), `"code":"upstream_unavailable"`)
				require.Contains(t, rec.Body.String(), "服务暂时不可用，请稍后再试。")
				require.NotContains(t, rec.Body.String(), "original diagnostic")
				require.Equal(t, int32(1), sends.Load(), "error display must not replay upstream work")
				wantBridges := int32(0)
				if cli {
					wantBridges = 1
				}
				require.Equal(t, wantBridges, bridges.Load())
			})
		}
	}
}

func TestOpenAIErrorMessagesHTTPClientErrorAccountMatrix(t *testing.T) {
	for _, kind := range []string{AccountTypeOAuth, AccountTypeAPIKey} {
		for _, cli := range []bool{false, true} {
			t.Run(kind+"/cli="+strconvBool(cli), func(t *testing.T) {
				var sends atomic.Int32
				upstream := &codexModelsHTTPUpstreamStub{do: func(_ *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
					sends.Add(1)
					return &http.Response{StatusCode: http.StatusBadRequest, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(`{"error":{"code":"session_account_lost","type":"invalid_request_error","message":"original session diagnostic","param":"input"}}`))}, nil
				}}
				svc, owner, bridges := safetyBufferingGatewayFixture(t, kind, cli, nil, upstream)
				rec := httptest.NewRecorder()
				c, _ := gin.CreateTestContext(rec)
				body := `{"model":"gpt-5.1","input":"hello","stream":false}`
				c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(body))
				c.Set(string(ctxkey.Platform), PlatformOpenAI)
				OpenAIErrorMessageOutput(svc.settingService, func(c *gin.Context) {
					_, err := svc.Forward(t.Context(), c, owner, []byte(body))
					require.Error(t, err)
				})(c)
				require.Equal(t, http.StatusBadRequest, rec.Code)
				require.JSONEq(t, `{"error":{"code":"session_account_lost","type":"server_error","message":"当前会话可能无法恢复，请新开会话继续。","param":"input"}}`, rec.Body.String())
				require.Equal(t, int32(1), sends.Load())
				wantBridges := int32(0)
				if cli {
					wantBridges = 1
				}
				require.Equal(t, wantBridges, bridges.Load())
			})
		}
	}
}

func TestOpenAIErrorMessagesStripsDiagnosticHeaders(t *testing.T) {
	svc, _ := safetyBufferingTestService(t)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	c.Set(string(ctxkey.Platform), PlatformOpenAI)
	OpenAIErrorMessageOutput(svc.settingService, func(c *gin.Context) {
		c.Header("Content-Type", "application/json")
		c.Header("WWW-Authenticate", "Bearer realm=secret")
		c.Header("Location", "https://upstream.example/secret")
		c.Header("Server-Timing", "secret")
		c.Header("Retry-After", "7")
		c.Status(http.StatusTooManyRequests)
		_, err := c.Writer.WriteString(`{"error":{"code":"rate_limit_exceeded","message":"secret"}}`)
		require.NoError(t, err)
	})(c)
	require.Equal(t, http.StatusTooManyRequests, rec.Code)
	for _, header := range []string{"Www-Authenticate", "Location", "Server-Timing"} {
		require.Empty(t, rec.Header().Get(header), header)
	}
	require.Equal(t, "7", rec.Header().Get("Retry-After"))
	require.NotContains(t, rec.Body.String(), "secret")
	require.Contains(t, rec.Body.String(), "当前请求受到频率限制，请稍后再试。")
}
