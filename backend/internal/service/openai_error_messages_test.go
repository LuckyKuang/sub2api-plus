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
	for _, tc := range []struct{ code, message string }{
		{"upstream_response_interrupted", "响应已中断，请输入「继续」重新提交。"},
		{"upstream_unavailable", "响应已中断，请输入「继续」重新提交。"},
		{"session_account_lost", "当前会话可能无法恢复，请新开会话继续。"},
	} {
		for _, envelope := range []string{
			`{"error":{"code":"%s","message":"%s","type":"upstream_error","param":"input","opaque":7}}`,
			`{"type":"error","sequence_number":9,"error":{"code":"%s","message":"%s","type":"upstream_error"}}`,
			`{"type":"error","code":"%s","message":"%s","sequence_number":9}`,
			`{"type":"response.failed","response":{"id":"resp_1","status":"failed","error":{"code":"%s","message":"%s","type":"upstream_error"},"usage":{"input_tokens":12,"output_tokens":3}},"opaque":true}`,
			`{"type":"response.failed","error":{"code":"%s","message":"%s","type":"upstream_error"},"opaque":true}`,
		} {
			input := fmt.Sprintf(envelope, tc.code, "original diagnostic")
			want := fmt.Sprintf(envelope, tc.code, tc.message)
			require.JSONEq(t, want, string(normalizeOpenAIErrorMessage([]byte(input), openAIErrorMessageSource{})))
		}
	}
	for _, input := range []string{
		`{"error":{"code":"upstream_stream_read_error","message":"original"}}`,
		`{"error":{"code":"UPSTREAM_UNAVAILABLE","message":"original"}}`,
		`{"error":{"code":"upstream_unavailable_extra","message":"original"}}`,
		`{"type":"response.output_item.done","item":{"error":{"code":"upstream_unavailable","message":"original"}}}`,
		`{"type":"future","error":{"code":"session_account_lost","message":"original"}}`,
		`{"type":"response.completed","response":{"error":{"code":"session_account_lost","message":"original"}}}`,
		`{"error":{"code":"upstream_unavailable","message":null}}`,
		`{"error":{"code":"upstream_unavailable"}}`,
		`{"error":{"code":42,"message":"original"}}`,
		`[{"error":{"code":"upstream_unavailable","message":"original"}}]`,
		`{"error":`, `[DONE]`,
	} {
		require.Equal(t, input, string(normalizeOpenAIErrorMessage([]byte(input), openAIErrorMessageSource{})))
	}
}

func TestOpenAIErrorMessagesLiveHTTPBoundary(t *testing.T) {
	for _, protocol := range []string{"json", "sse", "sse-eof"} {
		for _, platform := range []string{PlatformOpenAI, PlatformAnthropic, PlatformGrok, ""} {
			t.Run(protocol+"/"+platform, func(t *testing.T) {
				svc, _ := safetyBufferingTestService(t)
				for _, initiallyEnabled := range []bool{false, true} {
					saveErrorMessages(t, svc.settingService, initiallyEnabled)
					rec := httptest.NewRecorder()
					c, _ := gin.CreateTestContext(rec)
					c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
					OpenAIErrorMessageOutput(svc.settingService, func(c *gin.Context) {
						// Selection happens after middleware installation.
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
						saveErrorMessages(t, svc.settingService, !initiallyEnabled)
						_, err = c.Writer.WriteString(payload[middle:])
						require.NoError(t, err)
					})(c)
					require.Equal(t, http.StatusBadGateway, rec.Code)
					if platform == PlatformOpenAI && !initiallyEnabled {
						require.Contains(t, rec.Body.String(), "响应已中断，请输入「继续」重新提交。")
						require.NotContains(t, rec.Body.String(), "original")
					} else {
						require.Contains(t, rec.Body.String(), "original")
					}
					require.Contains(t, rec.Body.String(), `"code":"upstream_unavailable"`)
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
		for _, enabled := range []bool{false, true} {
			t.Run(contentType+"/enabled="+strconvBool(enabled), func(t *testing.T) {
				svc, _ := safetyBufferingTestService(t)
				saveErrorMessages(t, svc.settingService, enabled)
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
				if !enabled {
					require.Equal(t, wire, rec.Body.String())
					return
				}
				body := strings.TrimSpace(strings.TrimPrefix(rec.Body.String(), "id: 42\nevent: error\ndata: "))
				require.Equal(t, "响应已中断，请输入「继续」重新提交。", gjson.Get(body, "error.message").String())
				require.Equal(t, opaque, gjson.Get(body, "opaque").String())
				require.Equal(t, "keep tool content", gjson.Get(body, "item.error.message").String())
				require.Equal(t, http.StatusBadGateway, rec.Code)
			})
		}
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
			for _, enabled := range []bool{false, true} {
				t.Run(kind+"/cli="+strconvBool(cli)+"/enabled="+strconvBool(enabled), func(t *testing.T) {
					var sends atomic.Int32
					upstream := &codexModelsHTTPUpstreamStub{do: func(req *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
						sends.Add(1)
						return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"text/event-stream"}}, Body: io.NopCloser(strings.NewReader("data: {\"type\":\"response.output_text.delta\",\"delta\":\"partial answer\"}\n\ndata: {\"type\":\"response.failed\",\"response\":{\"id\":\"resp_error\",\"status\":\"failed\",\"error\":{\"code\":\"upstream_unavailable\",\"message\":\"original diagnostic\"},\"usage\":{\"input_tokens\":12,\"output_tokens\":3}}}\n\n"))}, nil
					}}
					svc, owner, bridges := safetyBufferingGatewayFixture(t, kind, cli, nil, upstream)
					saveSafetyBuffering(t, svc.settingService, false)
					saveErrorMessages(t, svc.settingService, enabled)
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
					if enabled {
						require.Contains(t, rec.Body.String(), "响应已中断，请输入「继续」重新提交。")
						require.NotContains(t, rec.Body.String(), "original diagnostic")
					} else {
						require.Contains(t, rec.Body.String(), "original diagnostic")
					}
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
}

func TestOpenAIErrorMessagesHTTPClientErrorAccountMatrix(t *testing.T) {
	for _, kind := range []string{AccountTypeOAuth, AccountTypeAPIKey} {
		for _, cli := range []bool{false, true} {
			for _, enabled := range []bool{false, true} {
				t.Run(kind+"/cli="+strconvBool(cli)+"/enabled="+strconvBool(enabled), func(t *testing.T) {
					var sends atomic.Int32
					upstream := &codexModelsHTTPUpstreamStub{do: func(_ *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
						sends.Add(1)
						return &http.Response{StatusCode: http.StatusBadRequest, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(`{"error":{"code":"session_account_lost","type":"invalid_request_error","message":"original session diagnostic","param":"input"}}`))}, nil
					}}
					svc, owner, bridges := safetyBufferingGatewayFixture(t, kind, cli, nil, upstream)
					saveErrorMessages(t, svc.settingService, enabled)
					rec := httptest.NewRecorder()
					c, _ := gin.CreateTestContext(rec)
					body := `{"model":"gpt-5.1","input":"hello","stream":false}`
					c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(body))
					c.Set(string(ctxkey.Platform), PlatformOpenAI)
					OpenAIErrorMessageOutput(svc.settingService, func(c *gin.Context) {
						_, err := svc.Forward(t.Context(), c, owner, []byte(body))
						require.Error(t, err)
					})(c)
					want := "original session diagnostic"
					if enabled {
						want = "当前会话可能无法恢复，请新开会话继续。"
					}
					require.Equal(t, http.StatusBadRequest, rec.Code)
					require.JSONEq(t, `{"error":{"code":"session_account_lost","type":"invalid_request_error","param":"input","message":"`+want+`"}}`, rec.Body.String())
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
}
