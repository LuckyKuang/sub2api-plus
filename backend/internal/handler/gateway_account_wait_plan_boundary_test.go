//go:build unit

package handler

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/config"
	"github.com/LuckyKuang/sub2api-plus/internal/pkg/ctxkey"
	"github.com/LuckyKuang/sub2api-plus/internal/pkg/tlsfingerprint"
	"github.com/LuckyKuang/sub2api-plus/internal/server/middleware"
	"github.com/LuckyKuang/sub2api-plus/internal/service"
	"github.com/LuckyKuang/sub2api-plus/internal/testutil/concurrencycache"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type handlerAccountWaitPlanCache struct {
	service.ConcurrencyCache
	queueFailure        error
	queueAttempts       atomic.Int32
	accountAcquisitions atomic.Int32
	userAcquisitions    atomic.Int32
	userReleases        atomic.Int32
}

func (c *handlerAccountWaitPlanCache) IncrementAccountWaitCount(ctx context.Context, accountID int64, maxWaiting int) (bool, error) {
	c.queueAttempts.Add(1)
	if c.queueFailure != nil {
		return false, c.queueFailure
	}
	return c.ConcurrencyCache.IncrementAccountWaitCount(ctx, accountID, maxWaiting)
}

func (c *handlerAccountWaitPlanCache) AcquireAccountSlot(ctx context.Context, accountID int64, limit int, owner string) (bool, error) {
	c.accountAcquisitions.Add(1)
	return c.ConcurrencyCache.AcquireAccountSlot(ctx, accountID, limit, owner)
}

func (c *handlerAccountWaitPlanCache) AcquireUserSlot(ctx context.Context, userID int64, limit int, owner string) (bool, error) {
	c.userAcquisitions.Add(1)
	return c.ConcurrencyCache.AcquireUserSlot(ctx, userID, limit, owner)
}

func (c *handlerAccountWaitPlanCache) ReleaseUserSlot(ctx context.Context, userID int64, owner string) error {
	c.userReleases.Add(1)
	return c.ConcurrencyCache.ReleaseUserSlot(ctx, userID, owner)
}

type handlerAccountWaitPlanUpstream struct {
	calls atomic.Int32
}

func (u *handlerAccountWaitPlanUpstream) Do(*http.Request, string, int64, int) (*http.Response, error) {
	u.calls.Add(1)
	return nil, errors.New("account queue rejection must precede upstream execution")
}

func (u *handlerAccountWaitPlanUpstream) DoWithTLS(req *http.Request, proxy string, accountID int64, concurrency int, _ *tlsfingerprint.Profile) (*http.Response, error) {
	return u.Do(req, proxy, accountID, concurrency)
}

// Exercise the real protocol handlers and scheduler rather than a route that
// directly calls the concurrency helper. A full account must not make these
// independent sending paths bypass the scheduler's bounded waiting plan.
func TestGatewayHandlersEnforceAccountWaitPlanBeforeWaiting(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, endpoint := range []struct {
		name     string
		path     string
		platform string
		body     string
		handler  func(*GatewayHandler) gin.HandlerFunc
	}{
		{
			name: "chat_completions", path: "/v1/chat/completions", platform: service.PlatformAnthropic,
			body:    `{"model":"claude-sonnet-4.5","stream":false,"messages":[{"role":"user","content":"hi"}]}`,
			handler: func(h *GatewayHandler) gin.HandlerFunc { return h.ChatCompletions },
		},
		{
			name: "responses", path: "/v1/responses", platform: service.PlatformAnthropic,
			body:    `{"model":"claude-sonnet-4.5","stream":false,"input":"hi"}`,
			handler: func(h *GatewayHandler) gin.HandlerFunc { return h.Responses },
		},
		{
			name: "systemone", path: "/v1/systemone", platform: service.PlatformTypeSafe,
			body:    validSystemOneHandlerBody,
			handler: func(h *GatewayHandler) gin.HandlerFunc { return h.SystemOne },
		},
	} {
		for _, scenario := range []struct {
			name         string
			waiting      int
			failure      error
			wantStatus   int
			responseCode string
		}{
			{name: "full_queue", waiting: 2, wantStatus: http.StatusTooManyRequests, responseCode: gatewayQueueFullCode},
			{name: "queue_dependency_unavailable", failure: errors.New("isolated account queue unavailable"), wantStatus: http.StatusServiceUnavailable},
		} {
			t.Run(endpoint.name+"/"+scenario.name, func(t *testing.T) {
				ctx := context.Background()
				const accountID, userID, groupID = int64(89401), int64(89402), int64(89403)
				raw, _ := concurrencycache.New(t)
				acquired, err := raw.AcquireAccountSlot(ctx, accountID, 1, "existing-running-owner")
				require.NoError(t, err)
				require.True(t, acquired)
				for n := 0; n < scenario.waiting; n++ {
					accepted, err := raw.IncrementAccountWaitCount(ctx, accountID, 2)
					require.NoError(t, err)
					require.True(t, accepted)
				}
				t.Cleanup(func() {
					for n := 0; n < scenario.waiting; n++ {
						require.NoError(t, raw.DecrementAccountWaitCount(context.Background(), accountID))
					}
					require.NoError(t, raw.ReleaseAccountSlot(context.Background(), accountID, "existing-running-owner"))
				})
				cache := &handlerAccountWaitPlanCache{ConcurrencyCache: raw, queueFailure: scenario.failure}
				concurrency := service.NewConcurrencyService(cache)
				group := &service.Group{
					ID: groupID, Hydrated: true, Platform: endpoint.platform,
					Status: service.StatusActive, RateMultiplier: 1,
				}
				account := &service.Account{
					ID: accountID, Platform: endpoint.platform, Type: service.AccountTypeAPIKey,
					Status: service.StatusActive, Schedulable: true, Concurrency: 1,
					GroupIDs: []int64{groupID}, AccountGroups: []service.AccountGroup{{AccountID: accountID, GroupID: groupID}},
					Credentials: map[string]any{"api_key": "queue-fixture", "base_url": "https://queue.invalid"},
				}
				cfg := &config.Config{RunMode: config.RunModeSimple}
				cfg.Default.RateMultiplier = 1
				cfg.Gateway.MaxBodySize = 1 << 20
				cfg.Gateway.Scheduling.LoadBatchEnabled = false
				cfg.Gateway.Scheduling.FallbackMaxWaiting = 2
				cfg.Gateway.Scheduling.FallbackWaitTimeout = 4 * time.Second
				billing := service.NewBillingCacheService(nil, nil, nil, nil, nil, nil, cfg, nil)
				t.Cleanup(billing.Stop)
				upstream := &handlerAccountWaitPlanUpstream{}
				snapshot := service.NewSchedulerSnapshotService(&fakeSchedulerCache{accounts: []*service.Account{account}}, nil, nil, nil, nil)
				gateway := service.NewGatewayService(nil, &fakeGroupRepo{group: group}, nil, nil, nil, nil, nil, nil, cfg, snapshot,
					concurrency, service.NewBillingService(cfg, nil), nil, billing, nil, upstream, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
				h := &GatewayHandler{
					gatewayService: gateway, billingCacheService: billing, cfg: cfg,
					concurrencyHelper: NewConcurrencyHelper(concurrency, SSEPingFormatNone, 0),
				}
				router := gin.New()
				router.Use(func(c *gin.Context) {
					c.Request = c.Request.WithContext(context.WithValue(c.Request.Context(), ctxkey.Group, group))
					c.Set(string(middleware.ContextKeyAPIKey), &service.APIKey{
						ID: 89404, UserID: userID, GroupID: &group.ID, Group: group, Status: service.StatusActive,
						User: &service.User{ID: userID, Status: service.StatusActive, Concurrency: 3, Balance: 100},
					})
					c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: userID, Concurrency: 3})
					c.Next()
				})
				router.POST(endpoint.path, endpoint.handler(h))
				server := httptest.NewServer(router)
				t.Cleanup(server.Close)
				client := server.Client()
				client.Timeout = 1500 * time.Millisecond
				started := time.Now()
				response, err := client.Post(server.URL+endpoint.path, "application/json", strings.NewReader(endpoint.body))
				require.NoError(t, err, "full or unconfirmed queues must reject without entering the four-second account wait")
				defer response.Body.Close()
				body, err := io.ReadAll(response.Body)
				require.NoError(t, err)
				require.Equal(t, scenario.wantStatus, response.StatusCode, string(body))
				require.Less(t, time.Since(started), time.Second, "account queue rejection must be immediate")
				if scenario.responseCode != "" {
					require.Contains(t, string(body), scenario.responseCode)
				}
				require.NotContains(t, string(body), "isolated account queue unavailable", "dependency details must not leak to clients")
				require.Equal(t, int32(1), cache.queueAttempts.Load(), "the actual handler must check the scheduler's queue bound exactly once")
				require.Equal(t, int32(2), cache.accountAcquisitions.Load(), "the scheduler and admission recheck must not start account polling")
				require.Equal(t, int32(1), cache.userAcquisitions.Load())
				require.Zero(t, upstream.calls.Load(), "rejected account waiters cannot reach any upstream sender")
				accountOwners, err := raw.GetAccountConcurrency(ctx, accountID)
				require.NoError(t, err)
				require.Equal(t, 1, accountOwners, "the preexisting running owner must remain intact")
				waiting, err := raw.GetAccountWaitingCount(ctx, accountID)
				require.NoError(t, err)
				require.Equal(t, scenario.waiting, waiting, "rejection must preserve preexisting queue entries")
				require.Eventually(t, func() bool {
					users, err := raw.GetUserConcurrency(context.Background(), userID)
					return err == nil && users == 0 && cache.userReleases.Load() == 1
				}, time.Second, 5*time.Millisecond, "both queue failures must release the request's user owner exactly once")
			})
		}
	}
}
