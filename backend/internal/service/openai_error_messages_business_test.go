//go:build unit

package service

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/LuckyKuang/sub2api-plus/internal/model"
	"github.com/LuckyKuang/sub2api-plus/internal/pkg/ctxkey"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// Acceptance: the wording switch also applies when the account's passthrough
// policy sanitizes HTTP errors. Sanitization, status and request count stay the
// same; the source diagnostic remains available only to operators.
func TestOpenAIErrorMessagesSanitizedHTTP(t *testing.T) {
	for _, kind := range []string{AccountTypeOAuth, AccountTypeAPIKey} {
		for _, cli := range []bool{false, true} {
			for _, enabled := range []bool{false, true} {
				for _, failure := range []struct {
					name, code, want string
					status           int
				}{
					{"session", "session_account_lost", "当前会话可能无法恢复，请新开会话继续。", 400},
					{"interrupted", "upstream_response_interrupted", "响应已中断，请输入「继续」重新提交。", 400},
					{"unavailable", "upstream_unavailable", "响应已中断，请输入「继续」重新提交。", 400},
					{"unknown", "other_error", "Upstream request failed", 400},
					{"case-sensitive", "UPSTREAM_UNAVAILABLE", "Upstream request failed", 400},
					{"oauth-server-unavailable", "upstream_unavailable", "响应已中断，请输入「继续」重新提交。", 503},
				} {
					if kind == AccountTypeAPIKey && failure.status == 503 {
						continue // API-key server failures belong to handler failover tests.
					}
					t.Run(fmt.Sprintf("%s/cli=%t/enabled=%t/%s", kind, cli, enabled, failure.name), func(t *testing.T) {
						var sends atomic.Int32
						upstream := &codexModelsHTTPUpstreamStub{do: func(_ *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
							sends.Add(1)
							body := `{"error":{"code":"` + failure.code + `","type":"upstream_error","message":"original private diagnostic"},"private":"must not escape"}`
							return &http.Response{StatusCode: failure.status, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(body))}, nil
						}}
						svc, owner, bridges := safetyBufferingGatewayFixture(t, kind, cli, nil, upstream)
						owner.Extra["openai_passthrough"] = true
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
						want := "Upstream request failed"
						if failure.status == 503 {
							want = "Upstream service temporarily unavailable"
						}
						if enabled {
							want = failure.want
						}
						require.Equal(t, failure.status, rec.Code)
						require.JSONEq(t, `{"error":{"type":"upstream_error","message":"`+want+`"}}`, rec.Body.String())
						require.Equal(t, int32(1), sends.Load(), "display policy must not replay the request")
						wantBridges := int32(0)
						if cli {
							wantBridges = 1
						}
						require.Equal(t, wantBridges, bridges.Load())
						msg, _ := c.Get(OpsUpstreamErrorMessageKey)
						require.Equal(t, "original private diagnostic", msg)
					})
				}
			}
		}
	}
}

// Acceptance: early header commits must never truncate or reject a projected
// response. A switch changed after headers but before the event uses its latest
// value. Exercise a real net/http connection, not Recorder's lenient writes.
func TestOpenAIErrorMessagesCommittedContentLength(t *testing.T) {
	for _, protocol := range []string{"json", "sse"} {
		for _, initial := range []bool{false, true} {
			for _, final := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/%t-to-%t", protocol, initial, final), func(t *testing.T) {
					svc, _ := safetyBufferingTestService(t)
					saveErrorMessages(t, svc.settingService, initial)
					wire := `{"error":{"code":"upstream_unavailable","type":"upstream_error","message":"x"}}`
					contentType := "application/json"
					if protocol == "sse" {
						wire = "data: " + wire + "\n\n"
						contentType = "text/event-stream"
					}
					errors := make(chan error, 1)
					server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						c, _ := gin.CreateTestContext(w)
						c.Request = r.WithContext(context.WithValue(r.Context(), ctxkey.Platform, PlatformOpenAI))
						OpenAIErrorMessageOutput(svc.settingService, func(c *gin.Context) {
							c.Header("Content-Type", contentType)
							c.Header("Content-Length", strconv.Itoa(len(wire)))
							c.Status(http.StatusBadGateway)
							c.Writer.WriteHeaderNow()
							c.Writer.Flush()
							saveErrorMessages(t, svc.settingService, final)
							_, err := c.Writer.WriteString(wire)
							errors <- err
						})(c)
					}))
					defer server.Close()
					resp, err := server.Client().Get(server.URL)
					require.NoError(t, err)
					defer resp.Body.Close()
					body, readErr := io.ReadAll(resp.Body)
					require.NoError(t, <-errors, "projected bytes must reach the socket")
					require.NoError(t, readErr)
					require.Equal(t, 502, resp.StatusCode)
					payload := strings.TrimSpace(strings.TrimPrefix(string(body), "data: "))
					want := "x"
					if final {
						want = "响应已中断，请输入「继续」重新提交。"
					}
					require.JSONEq(t, `{"error":{"code":"upstream_unavailable","type":"upstream_error","message":"`+want+`"}}`, payload)
				})
			}
		}
	}
}

// Source-code matching must not be broadened to error.type or embedded content.
func TestOpenAIErrorMessagesTypeIsNotCode(t *testing.T) {
	input := `{"error":{"type":"upstream_unavailable","message":"keep diagnostic"}}`
	require.Equal(t, input, string(normalizeOpenAIErrorMessage([]byte(input), openAIErrorMessageSource{})))
}

type errorMessageFragmentWriter struct {
	gin.ResponseWriter
	betweenWrites func()
}

func (w *errorMessageFragmentWriter) Write(p []byte) (int, error) {
	middle := len(p) / 2
	n, err := w.ResponseWriter.Write(p[:middle])
	if err != nil {
		return n, err
	}
	w.betweenWrites()
	m, err := w.ResponseWriter.Write(p[middle:])
	return n + m, err
}

// Sanitized output can be staged while the switch changes. The final release
// must use the new value in both directions, not the value at classification.
func TestOpenAIErrorMessagesSanitizedPendingToggle(t *testing.T) {
	for _, initial := range []bool{false, true} {
		t.Run(fmt.Sprintf("%t-to-%t", initial, !initial), func(t *testing.T) {
			svc, _ := safetyBufferingTestService(t)
			saveErrorMessages(t, svc.settingService, initial)
			rec := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(rec)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
			c.Set(string(ctxkey.Platform), PlatformOpenAI)
			OpenAIErrorMessageOutput(svc.settingService, func(c *gin.Context) {
				c.Writer = &errorMessageFragmentWriter{ResponseWriter: c.Writer, betweenWrites: func() {
					require.Empty(t, rec.Body.String(), "incomplete sanitized error must remain pending")
					saveErrorMessages(t, svc.settingService, !initial)
				}}
				body := []byte(`{"error":{"code":"upstream_unavailable","message":"original private diagnostic"}}`)
				err := svc.handleErrorResponsePassthrough(t.Context(), &http.Response{StatusCode: 400, Header: make(http.Header)}, c, &Account{ID: 7, Platform: PlatformOpenAI, Type: AccountTypeOAuth}, nil, body)
				require.Error(t, err)
			})(c)
			want := "Upstream request failed"
			if !initial {
				want = "响应已中断，请输入「继续」重新提交。"
			}
			require.Equal(t, 400, rec.Code)
			require.JSONEq(t, `{"error":{"type":"upstream_error","message":"`+want+`"}}`, rec.Body.String())
		})
	}
}

// An administrator's error rule still controls status and monitoring. The
// independent wording switch controls the final message for selected codes.
func TestOpenAIErrorMessagesRuleOutput(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		t.Run(fmt.Sprintf("enabled=%t", enabled), func(t *testing.T) {
			svc, _ := safetyBufferingTestService(t)
			saveErrorMessages(t, svc.settingService, enabled)
			rules := &ErrorPassthroughService{}
			rules.setLocalCache([]*model.ErrorPassthroughRule{newNonFailoverPassthroughRule(400, "original diagnostic", 418, "configured rule wording")})
			rec := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(rec)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
			c.Set(string(ctxkey.Platform), PlatformOpenAI)
			BindErrorPassthroughService(c, rules)
			OpenAIErrorMessageOutput(svc.settingService, func(c *gin.Context) {
				resp := &http.Response{StatusCode: 400, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"error":{"code":"session_account_lost","message":"original diagnostic"}}`))}
				_, err := svc.handleErrorResponse(t.Context(), resp, c, &Account{ID: 7, Platform: PlatformOpenAI, Type: AccountTypeAPIKey}, nil)
				require.ErrorContains(t, err, "original diagnostic")
			})(c)
			want := "configured rule wording"
			if enabled {
				want = "当前会话可能无法恢复，请新开会话继续。"
			}
			require.Equal(t, 418, rec.Code)
			require.JSONEq(t, `{"error":{"type":"upstream_error","message":"`+want+`"}}`, rec.Body.String())
		})
	}
}

// A failure before the first answer can become a JSON response under an error
// rule. It must not lose the session wording or replay the rejected generation.
func TestOpenAIErrorMessagesFirstFailureRule(t *testing.T) {
	for _, kind := range []string{AccountTypeOAuth, AccountTypeAPIKey} {
		for _, passthrough := range []bool{false, true} {
			for _, cli := range []bool{false, true} {
				for _, enabled := range []bool{false, true} {
					t.Run(fmt.Sprintf("%s/passthrough=%t/cli=%t/enabled=%t", kind, passthrough, cli, enabled), func(t *testing.T) {
						var sends atomic.Int32
						upstream := &codexModelsHTTPUpstreamStub{do: func(_ *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
							sends.Add(1)
							return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"text/event-stream"}}, Body: io.NopCloser(strings.NewReader("data: " + `{"type":"response.failed","response":{"id":"resp_rejected","status":"failed","error":{"code":"session_account_lost","type":"invalid_request_error","message":"original diagnostic"}}}` + "\n\n"))}, nil
						}}
						svc, owner, bridges := safetyBufferingGatewayFixture(t, kind, cli, nil, upstream)
						owner.Extra["openai_passthrough"] = passthrough
						saveErrorMessages(t, svc.settingService, enabled)
						rules := &ErrorPassthroughService{}
						rules.setLocalCache([]*model.ErrorPassthroughRule{newNonFailoverPassthroughRule(400, "original diagnostic", 418, "configured rule wording")})
						rec := httptest.NewRecorder()
						c, _ := gin.CreateTestContext(rec)
						body := `{"model":"gpt-5.1","input":"hello","stream":true}`
						c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(body))
						c.Set(string(ctxkey.Platform), PlatformOpenAI)
						BindErrorPassthroughService(c, rules)
						OpenAIErrorMessageOutput(svc.settingService, func(c *gin.Context) {
							_, err := svc.Forward(t.Context(), c, owner, []byte(body))
							require.Error(t, err)
						})(c)
						want := "configured rule wording"
						if enabled {
							want = "当前会话可能无法恢复，请新开会话继续。"
						}
						require.Equal(t, 418, rec.Code)
						require.JSONEq(t, `{"error":{"type":"upstream_error","message":"`+want+`"}}`, rec.Body.String())
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
}

// A stream=false request may receive an SSE rejection upstream. Remote compact
// may already have sent a keepalive, requiring a failed SSE terminal instead
// of a JSON error. Both clients must see the selected session message.
func TestOpenAIErrorMessagesNonStreamingFailure(t *testing.T) {
	for _, passthrough := range []bool{false, true} {
		for _, enabled := range []bool{false, true} {
			for _, compact := range []bool{false, true} {
				t.Run(fmt.Sprintf("passthrough=%t/enabled=%t/compact=%t", passthrough, enabled, compact), func(t *testing.T) {
					svc, _ := safetyBufferingTestService(t)
					saveErrorMessages(t, svc.settingService, enabled)
					rec := httptest.NewRecorder()
					c, _ := gin.CreateTestContext(rec)
					c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
					c.Set(string(ctxkey.Platform), PlatformOpenAI)
					OpenAIErrorMessageOutput(svc.settingService, func(c *gin.Context) {
						if compact {
							MarkOpenAICompactClientStream(c)
							keepalive := &openAICompactSSEKeepalive{writer: c.Writer, stop: make(chan struct{})}
							c.Set(openAICompactSSEKeepaliveKey, keepalive)
							require.True(t, keepalive.beat())
							defer keepalive.Stop()
						}
						body := []byte("data: " + `{"type":"response.failed","response":{"id":"resp_session","status":"failed","error":{"code":"session_account_lost","type":"invalid_request_error","message":"original session diagnostic"}}}` + "\n\n")
						resp := &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"text/event-stream"}}}
						owner := &Account{ID: 7, Platform: PlatformOpenAI, Type: AccountTypeAPIKey}
						var err error
						if passthrough {
							_, err = svc.handlePassthroughSSEToJSON(resp, c, owner, body, "gpt-5.1", "gpt-5.1")
						} else {
							_, err = svc.handleSSEToJSON(resp, c, owner, body, "gpt-5.1", "gpt-5.1")
						}
						require.ErrorContains(t, err, "original session diagnostic")
					})(c)
					want := "original session diagnostic"
					if enabled {
						want = "当前会话可能无法恢复，请新开会话继续。"
					}
					if compact {
						require.Equal(t, 200, rec.Code)
						require.Contains(t, rec.Body.String(), "event: response.failed")
						require.Contains(t, rec.Body.String(), `"code":"upstream_error"`)
						require.Contains(t, rec.Body.String(), want)
						if enabled {
							require.NotContains(t, rec.Body.String(), "original session diagnostic")
						}
					} else {
						require.Equal(t, 502, rec.Code)
						require.JSONEq(t, `{"error":{"type":"upstream_error","message":"`+want+`"}}`, rec.Body.String())
					}
				})
			}
		}
	}
}
