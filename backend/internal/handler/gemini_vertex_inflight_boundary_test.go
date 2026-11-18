//go:build unit

package handler

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/LuckyKuang/sub2api-plus/internal/config"
	"github.com/LuckyKuang/sub2api-plus/internal/pkg/ctxkey"
	"github.com/LuckyKuang/sub2api-plus/internal/repository"
	"github.com/LuckyKuang/sub2api-plus/internal/server/middleware"
	"github.com/LuckyKuang/sub2api-plus/internal/service"
	"github.com/alicebob/miniredis/v2"
	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

// Standard Gemini 3.8 Flash's 1000 output tokens fit $0.0039 of available
// balance. The selected US endpoint's 10% higher default charge cannot fit.
// The real handlers must release this request's owners before any upstream send.
func TestGeminiHandlersRejectUnfundedVertexMultiRegionBeforeForwarding(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, endpoint := range []string{"messages", "chat/completions", "responses", "gemini_native", "gemini_native_channel_mapping"} {
		t.Run(endpoint, func(t *testing.T) {
			const userID, groupID, accountID = int64(89901), int64(89902), int64(89903)
			const balance = .004
			ctx := context.Background()
			mr := miniredis.RunT(t)
			rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
			t.Cleanup(func() { _ = rdb.Close() })
			cache := repository.NewBillingCache(rdb)
			require.NoError(t, cache.SetUserBalance(ctx, userID, balance))
			raw := repository.NewConcurrencyCache(rdb, 15, 900)
			owned, err := raw.AcquireAccountSlot(ctx, accountID, 2, "foreign-account-owner")
			require.NoError(t, err)
			require.True(t, owned)
			counts := &handlerAccountWaitPlanCache{ConcurrencyCache: raw}
			concurrency := service.NewConcurrencyService(counts)
			cfg := &config.Config{}
			cfg.Default.RateMultiplier = 1
			cfg.Gateway.MaxBodySize = 1 << 20
			cfg.Billing.InflightReservation = config.InflightReservationConfig{Enabled: true, TTLSeconds: 60, DefaultMaxTokens: 1000, MaxInputTokens: 1, MaxOutputTokens: 1000}
			billing := service.NewBillingCacheService(cache, nil, nil, nil, nil, nil, cfg, nil)
			t.Cleanup(billing.Stop)
			group := &service.Group{ID: groupID, Platform: service.PlatformGemini, Status: service.StatusActive, Hydrated: true, RateMultiplier: 1}
			key := &service.APIKey{ID: 89904, UserID: userID, Group: group, GroupID: &group.ID, Status: service.StatusActive,
				User: &service.User{ID: userID, Status: service.StatusActive, Concurrency: 3, Balance: balance}}
			foreign, err := billing.ReserveInflight(ctx, key.User, group, nil, .0001)
			require.NoError(t, err)
			require.NotNil(t, foreign)
			t.Cleanup(foreign.HandlerDone)
			foreignHolds, err := rdb.HGetAll(ctx, "billing:inflight_amt:{89901}").Result()
			require.NoError(t, err)
			account := &service.Account{ID: accountID, Platform: service.PlatformGemini, Type: service.AccountTypeServiceAccount,
				Status: service.StatusActive, Schedulable: true, Concurrency: 2, GroupIDs: []int64{groupID},
				AccountGroups: []service.AccountGroup{{AccountID: accountID, GroupID: groupID}},
				Credentials:   map[string]any{"auth_mode": "apikey", "api_key": "vertex-inflight-fixture", "project_id": "p1", "location": "us"}}
			upstream := &handlerAccountWaitPlanUpstream{}
			scheduler := service.NewSchedulerSnapshotService(&fakeSchedulerCache{accounts: []*service.Account{account}}, nil, nil, nil, cfg)
			prices := service.NewBillingService(cfg, nil)
			var channels *service.ChannelService
			if endpoint == "gemini_native_channel_mapping" {
				// The handler forwards only A -> B -> D. The separate B -> C
				// channel rule is for clients that actually request B; applying
				// it again would reserve a cheaper unsupported endpoint.
				channels = service.NewChannelService(&openAIWSUsageHandlerChannelRepoStub{
					groupPlatforms: map[int64]string{groupID: service.PlatformGemini},
					channels: []service.Channel{{ID: 89905, Status: service.StatusActive, GroupIDs: []int64{groupID},
						BillingModelSource: service.BillingModelSourceChannelMapped,
						ModelMapping: map[string]map[string]string{service.PlatformGemini: {
							"gemini-3.8-flash": "gemini-3.7-flash", "gemini-3.7-flash": "gemini-2.5-flash-lite",
						}}}},
				}, nil, nil, nil, nil)
				account.Credentials["model_mapping"] = map[string]any{"gemini-3.7-flash": "gemini-4-argon"}
			}
			gateway := service.NewGatewayService(nil, &fakeGroupRepo{group: group}, nil, nil, nil, nil, nil, nil, cfg, scheduler,
				concurrency, prices, nil, billing, nil, upstream, nil, nil, nil, nil, nil, nil, nil, channels,
				service.NewModelPricingResolver(channels, prices), nil, nil, nil)
			h := &GatewayHandler{gatewayService: gateway, billingCacheService: billing, cfg: cfg, maxAccountSwitches: 2, maxAccountSwitchesGemini: 2,
				concurrencyHelper:   NewConcurrencyHelper(concurrency, SSEPingFormatNone, 0),
				geminiCompatService: service.NewGeminiMessagesCompatService(nil, nil, nil, scheduler, nil, nil, upstream, nil, cfg)}
			body := `{"model":"gemini-3.8-flash","max_tokens":1000,"messages":[{"role":"user","content":"hi"}]}`
			path, route := "/v1/"+endpoint, "/v1/"+endpoint
			handle := h.Messages
			switch endpoint {
			case "chat/completions":
				body = `{"model":"gemini-3.8-flash","max_completion_tokens":1000,"messages":[{"role":"user","content":"hi"}]}`
				handle = h.ChatCompletions
			case "responses":
				body = `{"model":"gemini-3.8-flash","max_output_tokens":1000,"input":"hi"}`
				handle = h.Responses
			case "gemini_native", "gemini_native_channel_mapping":
				body = `{"contents":[{"role":"user","parts":[{"text":"hi"}]}],"generationConfig":{"maxOutputTokens":1000}}`
				path, route = "/v1beta/models/gemini-3.8-flash:generateContent", "/v1beta/models/*modelAction"
				handle = h.GeminiV1BetaModels
			}
			router := gin.New()
			router.Use(func(c *gin.Context) {
				c.Request = c.Request.WithContext(context.WithValue(c.Request.Context(), ctxkey.Group, group))
				c.Set(string(middleware.ContextKeyAPIKey), key)
				c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: userID, Concurrency: 3})
				c.Next()
			})
			router.POST(route, handle)
			recorder := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
			request.Header.Set("Content-Type", "application/json")
			router.ServeHTTP(recorder, request)
			require.Equal(t, http.StatusForbidden, recorder.Code, recorder.Body.String())
			if strings.HasPrefix(endpoint, "gemini_native") {
				require.Contains(t, recorder.Body.String(), "PERMISSION_DENIED")
			} else {
				require.Contains(t, recorder.Body.String(), "billing_error")
			}
			require.Equal(t, int32(1), counts.accountAcquisitions.Load(), "reconcile the selected endpoint before binding or sending; never fail over a billing rejection")
			require.Zero(t, upstream.calls.Load())
			owners, err := raw.GetAccountConcurrency(ctx, accountID)
			require.NoError(t, err)
			require.Equal(t, 1, owners, "preserve the foreign account owner")
			users, err := raw.GetUserConcurrency(ctx, userID)
			require.NoError(t, err)
			require.Zero(t, users)
			remaining, err := rdb.HGetAll(ctx, fmt.Sprintf("billing:inflight_amt:{%d}", userID)).Result()
			require.NoError(t, err)
			require.Equal(t, foreignHolds, remaining)
			currentBalance, err := cache.GetUserBalance(ctx, userID)
			require.NoError(t, err)
			require.Equal(t, balance, currentBalance)
		})
	}
}
