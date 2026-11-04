//go:build unit

package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/LuckyKuang/sub2api-plus/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type errorMessageSettingsRepo struct{ service.SettingRepository }

func (*errorMessageSettingsRepo) GetMultiple(context.Context, []string) (map[string]string, error) {
	return map[string]string{service.SettingKeyOpenAIErrorMessagesNormalized: "true"}, nil
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
