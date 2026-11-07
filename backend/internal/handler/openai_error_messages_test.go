//go:build unit

package handler

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/LuckyKuang/sub2api-plus/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

type errorMessageSettingsRepo struct {
	service.SettingRepository
	disabled bool
}

func (r *errorMessageSettingsRepo) GetMultiple(context.Context, []string) (map[string]string, error) {
	return map[string]string{service.SettingKeyOpenAIErrorMessagesNormalized: fmt.Sprint(!r.disabled)}, nil
}

// Exercise the production route composition and actual queued ops entry. The
// admin record must retain its original diagnostic even for a handler-generated
// error emitted after Forward's scope, and a final SSE frame without a blank line.
func TestOpenAIErrorMessagesPreserveOpsDiagnostics(t *testing.T) {
	for _, protocol := range []string{"json", "sse", "sse-eof"} {
		t.Run(protocol, func(t *testing.T) {
			setupOpsErrorLogTestQueue(t, 2)
			ops := service.NewOpsService(nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
			settings := service.NewSettingService(&errorMessageSettingsRepo{}, nil)
			router := gin.New()
			router.Use(GatewayOpsErrorLoggerMiddleware(ops, settings))
			router.POST("/v1/responses", func(c *gin.Context) {
				setOpsSelectedAccount(c, 101, service.PlatformOpenAI)
				if protocol == "json" {
					c.Data(http.StatusBadRequest, "application/json", []byte(`{"error":{"code":"session_account_lost","type":"invalid_request_error","message":"original session diagnostic"}}`))
					return
				}
				c.Header("Content-Type", "text/event-stream")
				c.Status(http.StatusOK)
				(&OpenAIGatewayHandler{}).handleStreamingAwareErrorWithCode(c, http.StatusBadGateway, "upstream_error", "upstream_unavailable", "original session diagnostic", true, true)
				if protocol == "sse-eof" {
					// An independently framed trailing error exercises the EOF path.
					_, _ = c.Writer.WriteString(`data: {"type":"error","error":{"code":"upstream_unavailable","type":"upstream_error","message":"original session diagnostic"}}`)
				}
			})
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/v1/responses", nil))
			want := "响应已中断，请输入「继续」重新提交。"
			if protocol == "json" {
				want = "当前会话可能无法恢复，请新开会话继续。"
				require.Equal(t, 400, rec.Code)
			} else {
				require.Equal(t, 200, rec.Code)
			}
			require.Contains(t, rec.Body.String(), want)
			require.NotContains(t, rec.Body.String(), "original session diagnostic")
			require.Equal(t, int64(1), OpsErrorLogQueueLength())
			entry := (<-opsErrorLogQueue).entry
			require.Equal(t, "original session diagnostic", entry.ErrorMessage)
			require.Contains(t, entry.ErrorBody, "original session diagnostic")
			require.NotContains(t, entry.ErrorBody, want)
		})
	}
}

// Acceptance: exhausted retries must display the requested message even when
// the gateway's existing error mapping omits the upstream code. The switch
// changes neither the HTTP/SSE failure contract nor the administrator record.
func TestOpenAIErrorMessagesFailoverExhausted(t *testing.T) {
	for _, platform := range []string{service.PlatformOpenAI, service.PlatformGrok} {
		for _, enabled := range []bool{false, true} {
			for _, streaming := range []bool{false, true} {
				for _, failure := range []struct{ code, want string }{
					{"upstream_unavailable", "响应已中断，请输入「继续」重新提交。"},
					{"upstream_response_interrupted", "响应已中断，请输入「继续」重新提交。"},
					{"session_account_lost", "当前会话可能无法恢复，请新开会话继续。"},
					{"other_error", "Upstream service temporarily unavailable"},
					{"UPSTREAM_UNAVAILABLE", "Upstream service temporarily unavailable"},
				} {
					t.Run(fmt.Sprintf("%s/enabled=%t/stream=%t/%s", platform, enabled, streaming, failure.code), func(t *testing.T) {
						setupOpsErrorLogTestQueue(t, 1)
						ops := service.NewOpsService(nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
						settings := service.NewSettingService(&errorMessageSettingsRepo{disabled: !enabled}, nil)
						router := gin.New()
						router.Use(GatewayOpsErrorLoggerMiddleware(ops, settings))
						router.POST("/v1/responses", func(c *gin.Context) {
							setOpsSelectedAccount(c, 101, platform)
							if streaming {
								c.Header("Content-Type", "text/event-stream")
								_, _ = c.Writer.WriteString("data: {\"type\":\"response.output_text.delta\",\"delta\":\"partial answer\"}\n\n")
							}
							(&OpenAIGatewayHandler{}).handleFailoverExhausted(c, &service.UpstreamFailoverError{
								StatusCode:   503,
								ResponseBody: []byte(`{"error":{"code":"` + failure.code + `","message":"original upstream diagnostic"}}`),
							}, streaming)
						})
						rec := httptest.NewRecorder()
						router.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/v1/responses", nil))
						want := "Upstream service temporarily unavailable"
						if enabled && platform == service.PlatformOpenAI {
							want = failure.want
						}
						if streaming {
							require.Equal(t, 200, rec.Code)
							require.Contains(t, rec.Body.String(), `"delta":"partial answer"`)
							require.Contains(t, rec.Body.String(), `"type":"response.failed"`)
							require.Contains(t, rec.Body.String(), `"code":"upstream_error"`)
							require.Contains(t, rec.Body.String(), want)
						} else {
							require.Equal(t, 502, rec.Code)
							require.JSONEq(t, `{"error":{"type":"upstream_error","message":"`+want+`"}}`, rec.Body.String())
							require.False(t, gjson.Get(rec.Body.String(), "error.code").Exists(), "do not change the existing error envelope")
						}
						require.Equal(t, int64(1), OpsErrorLogQueueLength())
						entry := (<-opsErrorLogQueue).entry
						if !streaming {
							require.NotNil(t, entry.UpstreamErrorMessage)
							require.Equal(t, "original upstream diagnostic", *entry.UpstreamErrorMessage)
						}
						require.Contains(t, entry.ErrorBody, "Upstream service temporarily unavailable")
						require.NotContains(t, entry.ErrorBody, "响应已中断")
					})
				}
			}
		}
	}
}
