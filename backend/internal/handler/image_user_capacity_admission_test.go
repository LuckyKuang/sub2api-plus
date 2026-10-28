//go:build unit || !integration

package handler

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/config"
	"github.com/LuckyKuang/sub2api-plus/internal/securityaudit"
	"github.com/LuckyKuang/sub2api-plus/internal/server/middleware"
	"github.com/LuckyKuang/sub2api-plus/internal/service"
	"github.com/LuckyKuang/sub2api-plus/internal/testutil/concurrencycache"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestImageEndpointsRejectFullUserBeforeWaitingForImageCapacity(t *testing.T) {
	// All three image entrypoints must reject a full user before touching the
	// independent image queue. The held image owner is never released to let a
	// rejected user through; the three real Redis user owners stay intact.
	for _, scenario := range []struct{ name, platform, path, body string }{
		{"openai-images", service.PlatformOpenAI, "/v1/images/generations", `{"model":"gpt-image-1","prompt":"draw a square","n":1}`},
		{"grok-images", service.PlatformGrok, "/v1/images/generations", `{"model":"grok-imagine-image","prompt":"draw a square","n":1}`},
		{"responses-image-intent", service.PlatformOpenAI, "/v1/responses", `{"model":"gpt-5.4","input":"draw a square","tools":[{"type":"image_generation"}]}`},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			pool, _ := concurrencycache.New(t)
			concurrency := service.NewConcurrencyService(pool)
			users := &wsGlobalConcurrencyUserRepo{}
			users.limit.Store(3)
			concurrency.SetUserRepository(users)
			var held []func()
			for range 3 {
				ctx, cancel := service.WithConcurrencyLeaseContext(context.Background())
				slot, err := concurrency.AcquireUserSlot(ctx, wsGlobalConcurrencyUserID, 99)
				require.NoError(t, err)
				require.True(t, slot.Acquired)
				held = append(held, slot.ReleaseFunc)
				t.Cleanup(func() { slot.ReleaseFunc(); cancel(nil) })
			}
			cfg := &config.Config{RunMode: config.RunModeSimple}
			cfg.Default.RateMultiplier = 1
			cfg.Gateway.ImageConcurrency = config.ImageConcurrencyConfig{Enabled: true, MaxConcurrentRequests: 1, OverflowMode: config.ImageConcurrencyOverflowModeWait, WaitTimeoutSeconds: 10, MaxWaitingRequests: 8}
			billing := service.NewBillingCacheService(nil, nil, nil, nil, nil, nil, cfg, nil)
			defer billing.Stop()
			engine := &pricingAdmissionEngine{kind: securityaudit.DecisionAllow}
			limiter := &imageConcurrencyLimiter{}
			releaseImage, acquired := limiter.TryAcquire(true, 1)
			require.True(t, acquired)
			defer releaseImage()
			h := &OpenAIGatewayHandler{gatewayService: &service.OpenAIGatewayService{}, billingCacheService: billing,
				apiKeyService: &service.APIKeyService{}, concurrencyHelper: NewConcurrencyHelper(concurrency, SSEPingFormatComment, time.Millisecond),
				securityAuditCoordinator: securityaudit.NewCoordinator(nil, engine), cfg: cfg, imageLimiter: limiter}
			router := gin.New()
			router.Use(func(c *gin.Context) {
				group := &service.Group{ID: 7301, Platform: scenario.platform, Hydrated: true, Status: service.StatusActive, RateMultiplier: 1, AllowImageGeneration: true}
				c.Set(string(middleware.ContextKeyAPIKey), &service.APIKey{ID: 7302, UserID: wsGlobalConcurrencyUserID, User: &service.User{ID: wsGlobalConcurrencyUserID, Balance: 100}, GroupID: &group.ID, Group: group})
				c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: wsGlobalConcurrencyUserID, Concurrency: 99})
				c.Next()
			})
			entry := h.Images
			if scenario.name == "grok-images" {
				entry = h.GrokImages
			}
			if scenario.name == "responses-image-intent" {
				entry = h.Responses
			}
			router.POST(scenario.path, entry)
			server := httptest.NewServer(router)
			defer server.Close()
			var imageQueueEntered atomic.Bool
			monitorCtx, stopMonitor := context.WithCancel(context.Background())
			monitorDone := make(chan struct{})
			go func() {
				defer close(monitorDone)
				ticker := time.NewTicker(time.Millisecond)
				defer ticker.Stop()
				for {
					limiter.mu.Lock()
					if limiter.waiting != 0 {
						imageQueueEntered.Store(true)
					}
					limiter.mu.Unlock()
					select {
					case <-monitorCtx.Done():
						return
					case <-ticker.C:
					}
				}
			}()
			defer func() { stopMonitor(); <-monitorDone }()
			client := server.Client()
			client.Timeout = time.Second
			request, err := http.NewRequest(http.MethodPost, server.URL+scenario.path, strings.NewReader(scenario.body))
			require.NoError(t, err)
			request.Header.Set("Content-Type", "application/json")
			started := time.Now()
			response, err := client.Do(request)
			require.NoError(t, err, "full user must get HTTP 429 while image capacity is still held")
			defer func() { _ = response.Body.Close() }()
			body, err := io.ReadAll(response.Body)
			require.NoError(t, err)
			require.Less(t, time.Since(started), time.Second)
			require.Equal(t, http.StatusTooManyRequests, response.StatusCode, string(body))
			require.Equal(t, gatewayConcurrencyLimitCode, gjson.GetBytes(body, "error.code").String())
			require.Contains(t, string(body), "user")
			require.EqualValues(t, 1, engine.calls.Load(), "ingress audit must still run")
			require.False(t, imageQueueEntered.Load(), "a full user must never join the image wait queue")
			limiter.mu.Lock()
			active, waiting := limiter.active, limiter.waiting
			limiter.mu.Unlock()
			require.Equal(t, 1, active, "the pre-existing image owner remains held")
			require.Zero(t, waiting)
			loads, err := pool.GetUsersLoadBatch(context.Background(), []service.UserWithConcurrency{{ID: wsGlobalConcurrencyUserID, MaxConcurrency: 3}})
			require.NoError(t, err)
			require.NotNil(t, loads[wsGlobalConcurrencyUserID])
			require.Equal(t, 3, loads[wsGlobalConcurrencyUserID].CurrentConcurrency)
			require.Zero(t, loads[wsGlobalConcurrencyUserID].WaitingCount)
			for _, release := range held {
				release()
				release()
			}
			count, err := pool.GetUserConcurrency(context.Background(), wsGlobalConcurrencyUserID)
			require.NoError(t, err)
			require.Zero(t, count)
		})
	}
}
