//go:build unit

package handler

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/LuckyKuang/sub2api-plus/internal/config"
	"github.com/LuckyKuang/sub2api-plus/internal/server/middleware"
	"github.com/LuckyKuang/sub2api-plus/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

type errorMessageFailoverUpstream struct {
	service.HTTPUpstream
	mu    sync.Mutex
	ids   []int64
	final string
	sse   bool
}

func (u *errorMessageFailoverUpstream) Do(_ *http.Request, _ string, id int64, _ int) (*http.Response, error) {
	u.mu.Lock()
	u.ids = append(u.ids, id)
	u.mu.Unlock()
	status := 503
	body := `{"error":{"code":"upstream_unavailable","message":"first account diagnostic"}}`
	if id == 9911 {
		if u.final == "healthy" {
			status = 200
			body = `{"id":"resp_healthy","object":"response","model":"gpt-5.2","status":"completed","output":[],"usage":{"input_tokens":1,"output_tokens":1,"total_tokens":2}}`
		} else {
			body = `{"error":{"code":"` + u.final + `","message":"final account diagnostic"}}`
		}
	}
	if u.sse {
		if status != 200 {
			body = `{"type":"response.failed","response":{"id":"resp_failed","status":"failed","error":` + gjson.Get(body, "error").Raw + `}}`
		} else {
			body = `{"type":"response.completed","response":` + body + `}`
		}
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"text/event-stream"}}, Body: io.NopCloser(strings.NewReader("data: " + body + "\n\n"))}, nil
	}
	return &http.Response{StatusCode: status, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(body))}, nil
}

// Acceptance is based on account recovery, not on individual helper branches:
// a selected error followed by success must succeed, an unrelated final failure
// must not inherit wording, and only the final selected failure is normalized.
func TestOpenAIErrorMessagesRealAccountFailover(t *testing.T) {
	for _, wire := range []string{"json", "sse"} {
		for _, passthrough := range []bool{false, true} {
			for _, enabled := range []bool{false, true} {
				for _, final := range []string{"healthy", "other_error", "upstream_unavailable"} {
					t.Run(fmt.Sprintf("wire=%s/passthrough=%t/enabled=%t/final=%s", wire, passthrough, enabled, final), func(t *testing.T) {
						groupID := int64(4203)
						accounts := []service.Account{}
						for i, id := range []int64{9910, 9911} {
							accounts = append(accounts, service.Account{
								ID: id, Name: fmt.Sprint(id), Platform: service.PlatformOpenAI, Type: service.AccountTypeAPIKey,
								Status: service.StatusActive, Schedulable: true, Priority: i + 1,
								Credentials: map[string]any{"api_key": "sk-test-only", "base_url": "https://api.example.test"},
								Extra:       map[string]any{"openai_passthrough": passthrough},
							})
						}
						cfg := &config.Config{RunMode: config.RunModeSimple}
						cfg.Default.RateMultiplier = 1
						cfg.Security.URLAllowlist.Enabled = false
						cfg.Gateway.MaxAccountSwitches = 1
						upstream := &errorMessageFailoverUpstream{final: final, sse: wire == "sse"}
						settings := service.NewSettingService(&errorMessageSettingsRepo{disabled: !enabled}, nil)
						billing := service.NewBillingCacheService(nil, nil, nil, nil, nil, nil, cfg, nil)
						t.Cleanup(billing.Stop)
						gateway := service.NewOpenAIGatewayService(
							&openAIWSFailoverHandlerAccountRepoStub{accounts: accounts}, nil, nil, nil, nil, nil, nil, cfg, nil, nil,
							service.NewBillingService(cfg, nil), nil, billing, upstream, &service.DeferredService{}, nil, nil, nil, nil, nil, nil, nil, nil,
						)
						h := NewOpenAIGatewayHandler(gateway, service.NewConcurrencyService(&metadataConcurrencyCache{}), billing,
							service.NewAPIKeyService(nil, nil, nil, nil, nil, nil, cfg), nil, nil, nil, nil, cfg)
						setupOpsErrorLogTestQueue(t, 4)
						ops := service.NewOpsService(nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
						router := gin.New()
						router.Use(GatewayOpsErrorLoggerMiddleware(ops, settings))
						rec := httptest.NewRecorder()
						request := httptest.NewRequest(http.MethodPost, "/openai/v1/responses", strings.NewReader(fmt.Sprintf(`{"model":"gpt-5.2","input":"hello","stream":%t}`, wire == "sse")))
						request.Header.Set("Content-Type", "application/json")
						router.POST("/openai/v1/responses", func(c *gin.Context) {
							c.Set(string(middleware.ContextKeyAPIKey), &service.APIKey{
								ID: 1803, GroupID: &groupID, User: &service.User{ID: 1703, Status: service.StatusActive},
								Group: &service.Group{ID: groupID, Platform: service.PlatformOpenAI, Status: service.StatusActive},
							})
							c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: 1703, Concurrency: 0})
							h.Responses(c)
						})
						router.ServeHTTP(rec, request)
						upstream.mu.Lock()
						ids := append([]int64(nil), upstream.ids...)
						upstream.mu.Unlock()
						require.Equal(t, []int64{9910, 9911}, ids, "display policy must not add retries or skip recovery: %s", rec.Body.String())
						if final == "healthy" {
							require.Equal(t, 200, rec.Code)
							if wire == "sse" {
								require.Contains(t, rec.Body.String(), `"type":"response.completed"`)
								require.Contains(t, rec.Body.String(), `"id":"resp_healthy"`)
							} else {
								require.Equal(t, "resp_healthy", gjson.Get(rec.Body.String(), "id").String())
								require.Equal(t, "completed", gjson.Get(rec.Body.String(), "status").String())
							}
							require.NotContains(t, rec.Body.String(), "响应已中断")
						} else {
							want := "Upstream service temporarily unavailable"
							if enabled && final == "upstream_unavailable" {
								want = "响应已中断，请输入「继续」重新提交。"
							}
							require.Equal(t, 502, rec.Code)
							require.Contains(t, rec.Header().Get("Content-Type"), "application/json", "an uncommitted SSE attempt must not label the final JSON error as an event stream")
							require.JSONEq(t, `{"error":{"type":"upstream_error","message":"`+want+`"}}`, rec.Body.String())
							require.Equal(t, int64(1), OpsErrorLogQueueLength())
							entry := (<-opsErrorLogQueue).entry
							require.NotNil(t, entry.UpstreamErrorMessage)
							require.Equal(t, "final account diagnostic", *entry.UpstreamErrorMessage)
							require.Contains(t, entry.ErrorBody, "Upstream service temporarily unavailable")
							require.NotContains(t, entry.ErrorBody, "响应已中断")
						}
					})
				}
			}
		}
	}
}
