//go:build unit

package handler

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/LuckyKuang/sub2api-plus/internal/config"
	middleware "github.com/LuckyKuang/sub2api-plus/internal/server/middleware"
	"github.com/LuckyKuang/sub2api-plus/internal/service"
	"github.com/LuckyKuang/sub2api-plus/internal/testutil"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestMediaUnfinishedCapacityLifecycleAndUnknownCreates(t *testing.T) {
	for _, supplier := range []string{"grok", "seedance"} {
		for _, scenario := range []string{"limit", "repository_failure", "pending_then_failed", "unknown_create", "server_error", "rejected_then_accepted"} {
			t.Run(supplier+"/"+scenario, func(t *testing.T) {
				platforms := []string{}
				if supplier == "seedance" {
					platforms = []string{service.PlatformOpenAI}
				}
				h, slots, _, upstream := newGrokMediaSlotHandler(t, false, false, platforms...)
				repo := &testutil.StubGatewayUnfinishedTaskRepo{}
				cfg := &config.Config{}
				cfg.Gateway.MaxUnfinishedTasksPerUser = 1
				h.unfinishedTasks = service.NewGatewayUnfinishedTaskService(repo, cfg)
				if scenario == "repository_failure" {
					repo.Err = errors.New("database down")
				}
				if scenario == "limit" {
					_, err := h.unfinishedTasks.Reserve(context.Background(), service.GatewayTaskOwner{UserID: 10, APIKeyID: 21}, service.GatewayTaskKindBatchImage)
					require.NoError(t, err)
				}
				state := "pending"
				upstream.call = func(r *http.Request, _ int64) (*http.Response, error) {
					if scenario == "unknown_create" {
						return nil, errors.New("response lost after write")
					}
					status := http.StatusOK
					body := `{"request_id":"task","status":"pending"}`
					if supplier == "seedance" {
						body = `{"id":"task","status":"queued"}`
					}
					if r.Method == http.MethodGet {
						body = `{"status":"` + state + `"}`
					}
					if scenario == "rejected_then_accepted" && upstream.calls == 1 {
						status = http.StatusUnauthorized
						body = `{"error":{"message":"credential rejected"}}`
					}
					if scenario == "server_error" {
						status = http.StatusServiceUnavailable
						body = `{"error":{"message":"server failure"}}`
					}
					return &http.Response{StatusCode: status, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(body))}, nil
				}
				newRequest := func(create bool) (*gin.Context, *httptest.ResponseRecorder) {
					c, w := grokMediaSlotContext(context.Background(), create)
					if supplier == "seedance" {
						key, _ := middleware.GetAPIKeyFromContext(c)
						key.Group.Platform = service.PlatformOpenAI
						key.Group.ModelPricing = seedanceTokenPriceCard(2e-6)
						method, body := http.MethodGet, ""
						if create {
							method = http.MethodPost
							body = `{"model":"doubao-seedance","content":[{"type":"text","text":"waves"}]}`
						}
						c.Request = httptest.NewRequest(method, "/api/v3/contents/generations/tasks", strings.NewReader(body))
						c.Params = gin.Params{{Key: "task_id", Value: "task"}}
					}
					return c, w
				}
				send := func(c *gin.Context, create bool) {
					if supplier == "seedance" {
						h.SeedanceTasks(c)
					} else if create {
						h.GrokVideoGeneration(c)
					} else {
						h.GrokVideoStatus(c)
					}
				}
				c, w := newRequest(true)
				send(c, true)
				if supplier == "seedance" && scenario == "rejected_then_accepted" {
					require.Equal(t, http.StatusUnauthorized, w.Code, w.Body.String())
					require.Equal(t, 1, upstream.calls, "native Seedance create preserves its non-retry error contract")
					require.Empty(t, repo.Active, "a trusted provider rejection confirms no accepted task")
					slots.assertReleased(t)
					return
				}
				switch scenario {
				case "limit", "repository_failure":
					expected := http.StatusTooManyRequests
					if scenario == "repository_failure" {
						expected = http.StatusServiceUnavailable
					}
					require.Equal(t, expected, w.Code, w.Body.String())
					require.Zero(t, upstream.calls)
					require.Zero(t, slots.acquired)
				case "unknown_create", "server_error":
					require.GreaterOrEqual(t, w.Code, 400, w.Body.String())
					require.Equal(t, 1, upstream.calls, "ambiguous async creation must never be repeated on another account")
					require.Len(t, repo.Active, 1)
					require.Len(t, repo.Uncertain, 1)
				default:
					require.Equal(t, 200, w.Code, w.Body.String())
					require.Len(t, repo.Active, 1)
					require.Empty(t, repo.Uncertain)
					if scenario == "rejected_then_accepted" {
						require.Equal(t, 2, upstream.calls, "a definite authentication rejection permits exactly one account switch")
					}
					c, w = newRequest(false)
					send(c, false)
					require.Equal(t, 200, w.Code, w.Body.String())
					require.Len(t, repo.Active, 1, "pending status is not completion")
					state = "failed"
					c, w = newRequest(false)
					send(c, false)
					require.Equal(t, 200, w.Code, w.Body.String())
					require.Empty(t, repo.Active, "confirmed provider failure frees durable capacity without inventing billing")
					c, w = newRequest(false)
					send(c, false)
					require.Equal(t, 200, w.Code, w.Body.String())
					require.Empty(t, repo.Active, "repeated terminal observation is idempotent")
				}
				slots.assertReleased(t)
			})
		}
	}
}

// A content range response or acceptance of a cancel command does not prove
// that the provider stopped a task. The confirmed terminal status does.
func TestMediaUnfinishedCapacityRequiresTerminalEvidenceForContentAndCancellation(t *testing.T) {
	for _, scenario := range []string{"grok_pending_range", "grok_done_range", "seedance_cancel_accepted", "seedance_cancel_confirmed"} {
		t.Run(scenario, func(t *testing.T) {
			isSeedance := strings.HasPrefix(scenario, "seedance")
			platforms := []string{}
			kind, task := service.GatewayTaskKindGrokVideo, "task"
			if isSeedance {
				platforms = []string{service.PlatformOpenAI}
				kind = service.GatewayTaskKindSeedance
				task = service.SeedanceTaskKey(task)
			}
			h, slots, _, upstream := newGrokMediaSlotHandler(t, false, false, platforms...)
			repo := &testutil.StubGatewayUnfinishedTaskRepo{}
			h.unfinishedTasks = service.NewGatewayUnfinishedTaskService(repo, nil)
			owner := service.GatewayTaskOwner{UserID: 10, APIKeyID: 20}
			reserved, err := h.unfinishedTasks.Reserve(context.Background(), owner, kind)
			require.NoError(t, err)
			require.NoError(t, h.unfinishedTasks.Bind(context.Background(), reserved, task))
			c, w := grokMediaSlotContext(context.Background(), false)
			key, _ := middleware.GetAPIKeyFromContext(c)
			if isSeedance {
				key.Group.Platform = service.PlatformOpenAI
				key.Group.ModelPricing = seedanceTokenPriceCard(2e-6)
				require.NoError(t, h.gatewayService.BindGrokMediaVideoRequestAccount(context.Background(), key.GroupID, task, 10, 20, 1))
				c.Request.Method = http.MethodDelete
				c.Params = gin.Params{{Key: "task_id", Value: "task"}}
			}
			upstream.call = func(r *http.Request, _ int64) (*http.Response, error) {
				status, body := 200, `{"status":"pending"}`
				if isSeedance {
					status = 202
					if scenario == "seedance_cancel_confirmed" {
						status = 204
						body = ""
					}
				} else if strings.HasSuffix(r.URL.Path, "/content") {
					status = 416
					body = "range not satisfiable"
				} else if scenario == "grok_done_range" {
					body = `{"status":"done","video":{"url":"https://vidgen.x.ai/test.mp4"}}`
				}
				return &http.Response{StatusCode: status, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(body))}, nil
			}
			if isSeedance {
				h.SeedanceTasks(c)
			} else {
				h.GrokVideoContent(c)
			}
			if strings.Contains(scenario, "confirmed") || strings.Contains(scenario, "done") {
				require.Empty(t, repo.Active, w.Body.String())
			} else {
				require.Len(t, repo.Active, 1, "an unfinished task cannot disappear on a content/cancel HTTP response")
			}
			slots.assertReleased(t)
		})
	}
}
