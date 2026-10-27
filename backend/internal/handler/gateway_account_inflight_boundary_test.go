//go:build unit

package handler

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
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

// With upstream-model billing explicitly selected, a model with catalog pricing
// can be mapped to a more expensive model by the selected account. Its initial
// estimate fits the available balance;
// the actual selected owner's estimate does not. Every independent generic
// sending path must reject before forwarding, return its own concurrency slots
// and monetary hold, and preserve other requests' owners. A billing rejection
// cannot be retried against another account.
func TestGatewayHandlersRejectUnfundedSelectedAccountBeforeForwarding(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, endpoint := range []struct {
		name, platform, requested, mapped, path string
		streaming                               bool
		body                                    func(bool) string
		handler                                 func(*GatewayHandler) gin.HandlerFunc
	}{
		{
			name: "messages", platform: service.PlatformAnthropic, requested: "claude-3-haiku", mapped: "claude-sonnet-4",
			path: "/v1/messages", streaming: true,
			body: func(stream bool) string {
				return fmt.Sprintf(`{"model":"claude-3-haiku","max_tokens":1000,"stream":%t,"messages":[{"role":"user","content":"hi"}]}`, stream)
			},
			handler: func(h *GatewayHandler) gin.HandlerFunc { return h.Messages },
		},
		{
			name: "chat_completions", platform: service.PlatformAnthropic, requested: "claude-3-haiku", mapped: "claude-sonnet-4",
			path: "/v1/chat/completions", streaming: true,
			body: func(stream bool) string {
				return fmt.Sprintf(`{"model":"claude-3-haiku","max_completion_tokens":1000,"stream":%t,"messages":[{"role":"user","content":"hi"}]}`, stream)
			},
			handler: func(h *GatewayHandler) gin.HandlerFunc { return h.ChatCompletions },
		},
		{
			name: "responses", platform: service.PlatformAnthropic, requested: "claude-3-haiku", mapped: "claude-sonnet-4",
			path: "/v1/responses", streaming: true,
			body: func(stream bool) string {
				return fmt.Sprintf(`{"model":"claude-3-haiku","max_output_tokens":1000,"stream":%t,"input":"hi"}`, stream)
			},
			handler: func(h *GatewayHandler) gin.HandlerFunc { return h.Responses },
		},
		{
			name: "gemini_messages", platform: service.PlatformGemini, requested: "gemini-3.8-flash", mapped: "gemini-3.1-pro",
			path: "/v1/messages", streaming: true,
			body: func(stream bool) string {
				return fmt.Sprintf(`{"model":"gemini-3.8-flash","max_tokens":1000,"stream":%t,"messages":[{"role":"user","content":"hi"}]}`, stream)
			},
			handler: func(h *GatewayHandler) gin.HandlerFunc { return h.Messages },
		},
		{
			name: "gemini_native", platform: service.PlatformGemini, requested: "gemini-3.8-flash", mapped: "gemini-3.1-pro",
			path: "/v1beta/models/gemini-3.8-flash:generateContent", streaming: true,
			body: func(bool) string {
				return `{"contents":[{"role":"user","parts":[{"text":"hi"}]}],"generationConfig":{"maxOutputTokens":1000}}`
			},
			handler: func(h *GatewayHandler) gin.HandlerFunc { return h.GeminiV1BetaModels },
		},
		{
			name: "gemini_cached_content_create", platform: service.PlatformGemini, requested: "gemini-3.8-flash", mapped: "gemini-3.1-pro",
			path: "/v1beta/cachedContents",
			body: func(bool) string {
				return `{"model":"models/gemini-3.8-flash","contents":[{"role":"user","parts":[{"text":"hi"}]}],"ttl":"60s"}`
			},
			handler: func(h *GatewayHandler) gin.HandlerFunc { return h.GeminiCachedContentsCreate },
		},
	} {
		for _, stream := range []bool{false, true} {
			if stream && !endpoint.streaming {
				continue
			}
			t.Run(fmt.Sprintf("%s/stream=%t", endpoint.name, stream), func(t *testing.T) {
				ctx := context.Background()
				const userID, groupID = int64(89601), int64(89602)
				const balance, foreignAmount = .006, .0001
				mr := miniredis.RunT(t)
				rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
				t.Cleanup(func() { _ = rdb.Close() })
				billingCache := repository.NewBillingCache(rdb)
				require.NoError(t, billingCache.SetUserBalance(ctx, userID, balance))
				rawConcurrency := repository.NewConcurrencyCache(rdb, 15, 900)
				owned, err := rawConcurrency.AcquireUserSlot(ctx, userID, 3, "foreign-user-owner")
				require.NoError(t, err)
				require.True(t, owned)
				counts := &handlerAccountWaitPlanCache{ConcurrencyCache: rawConcurrency}
				concurrency := service.NewConcurrencyService(counts)
				group := &service.Group{ID: groupID, Hydrated: true, Platform: endpoint.platform, Status: service.StatusActive, RateMultiplier: 1}
				var accounts []*service.Account
				for _, accountID := range []int64{89603, 89604} {
					owned, err := rawConcurrency.AcquireAccountSlot(ctx, accountID, 2, "foreign-account-owner")
					require.NoError(t, err)
					require.True(t, owned)
					accounts = append(accounts, &service.Account{
						ID: accountID, Platform: endpoint.platform, Type: service.AccountTypeAPIKey,
						Status: service.StatusActive, Schedulable: true, Concurrency: 2,
						GroupIDs: []int64{groupID}, AccountGroups: []service.AccountGroup{{AccountID: accountID, GroupID: groupID}},
						Credentials: map[string]any{
							"api_key": "inflight-owner-fixture", "base_url": "https://inflight-owner.invalid",
							"model_mapping": map[string]any{endpoint.requested: endpoint.mapped},
							service.GeminiExplicitCacheUpstreamCredentialKey: true,
						},
					})
				}
				cfg := &config.Config{}
				cfg.Default.RateMultiplier = 1
				cfg.Gateway.MaxBodySize = 1 << 20
				cfg.Billing.InflightReservation = config.InflightReservationConfig{Enabled: true, TTLSeconds: 60, DefaultMaxTokens: 1000}
				billing := service.NewBillingCacheService(billingCache, nil, nil, nil, nil, nil, cfg, nil)
				t.Cleanup(billing.Stop)
				key := &service.APIKey{
					ID: 89605, UserID: userID, GroupID: &group.ID, Group: group, Status: service.StatusActive,
					User: &service.User{ID: userID, Status: service.StatusActive, Concurrency: 3, Balance: balance},
				}
				foreignHold, err := billing.ReserveInflight(ctx, key.User, group, nil, foreignAmount)
				require.NoError(t, err)
				require.NotNil(t, foreignHold)
				t.Cleanup(foreignHold.HandlerDone)
				foreignReservations, err := rdb.HGetAll(ctx, "billing:inflight_amt:{89601}").Result()
				require.NoError(t, err)
				require.Len(t, foreignReservations, 1)
				upstream := &handlerAccountWaitPlanUpstream{}
				snapshot := service.NewSchedulerSnapshotService(&fakeSchedulerCache{accounts: accounts}, nil, nil, nil, nil)
				prices := service.NewBillingService(cfg, nil)
				channels := service.NewChannelService(&openAIWSUsageHandlerChannelRepoStub{
					groupPlatforms: map[int64]string{groupID: endpoint.platform},
					channels: []service.Channel{{ID: 89606, Status: service.StatusActive, GroupIDs: []int64{groupID},
						BillingModelSource: service.BillingModelSourceUpstream}},
				}, nil, nil, nil, nil)
				gateway := service.NewGatewayService(nil, &fakeGroupRepo{group: group}, nil, nil, nil, nil, nil, nil, cfg, snapshot,
					concurrency, prices, nil, billing, nil, upstream, nil, nil, nil, nil, nil, nil, nil,
					channels, service.NewModelPricingResolver(channels, prices), nil, nil, nil)
				cacheRecords := &geminiCachedContentHandlerRepoStub{}
				h := &GatewayHandler{
					gatewayService: gateway, billingCacheService: billing, cfg: cfg,
					concurrencyHelper:          NewConcurrencyHelper(concurrency, SSEPingFormatNone, 0),
					geminiCompatService:        service.NewGeminiMessagesCompatService(nil, nil, nil, snapshot, nil, nil, upstream, nil, cfg),
					geminiCachedContentService: service.NewGeminiCachedContentService(cacheRecords, nil, cfg),
					maxAccountSwitches:         2, maxAccountSwitchesGemini: 2,
				}
				router := gin.New()
				router.Use(func(c *gin.Context) {
					requestCtx := context.WithValue(c.Request.Context(), ctxkey.Group, group)
					c.Request = c.Request.WithContext(requestCtx)
					c.Set(string(middleware.ContextKeyAPIKey), key)
					c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: userID, Concurrency: 3})
					c.Next()
				})
				path := endpoint.path
				if endpoint.name == "gemini_native" {
					router.POST("/v1beta/models/*modelAction", endpoint.handler(h))
					if stream {
						path = strings.ReplaceAll(path, ":generateContent", ":streamGenerateContent")
					}
				} else {
					router.POST(path, endpoint.handler(h))
				}
				recorder := httptest.NewRecorder()
				request := httptest.NewRequest(http.MethodPost, path, strings.NewReader(endpoint.body(stream)))
				request.Header.Set("Content-Type", "application/json")
				router.ServeHTTP(recorder, request)
				require.Equal(t, http.StatusForbidden, recorder.Code, recorder.Body.String())
				if endpoint.name == "gemini_native" || endpoint.name == "gemini_cached_content_create" {
					require.Contains(t, recorder.Body.String(), "PERMISSION_DENIED", "preserve the native Google billing error contract")
				} else {
					require.Contains(t, recorder.Body.String(), "billing_error", "preserve each endpoint's billing error contract")
				}
				require.Zero(t, upstream.calls.Load(), "unfunded mapped models must never be sent upstream")
				require.Empty(t, cacheRecords.records, "rejected cache creates cannot persist a cache owner")
				require.Equal(t, int32(1), counts.accountAcquisitions.Load(), "the initial retail estimate must pass; selected-account rejection must not fail over")
				require.Equal(t, int32(1), counts.userAcquisitions.Load())
				require.Zero(t, counts.queueAttempts.Load(), "a billing rejection must not enter an account queue")
				for _, account := range accounts {
					owners, err := rawConcurrency.GetAccountConcurrency(ctx, account.ID)
					require.NoError(t, err)
					require.Equal(t, 1, owners, "release only this request's selected-account owner")
				}
				users, err := rawConcurrency.GetUserConcurrency(ctx, userID)
				require.NoError(t, err)
				require.Equal(t, 1, users, "preserve the other request's global user owner")
				require.Equal(t, int32(1), counts.userReleases.Load())
				remaining, err := rdb.HGetAll(ctx, "billing:inflight_amt:{89601}").Result()
				require.NoError(t, err)
				require.Equal(t, foreignReservations, remaining, "return the rejected request's reservation without changing another owner's hold")
				for _, amount := range remaining {
					parsed, err := strconv.ParseFloat(amount, 64)
					require.NoError(t, err)
					require.Equal(t, foreignAmount, parsed)
				}
				currentBalance, err := billingCache.GetUserBalance(ctx, userID)
				require.NoError(t, err)
				require.Equal(t, balance, currentBalance, "rejection cannot charge any usage")
			})
		}
	}
}
