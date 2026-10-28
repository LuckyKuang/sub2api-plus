//go:build unit

package handler

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/config"
	"github.com/LuckyKuang/sub2api-plus/internal/pkg/ctxkey"
	"github.com/LuckyKuang/sub2api-plus/internal/repository"
	"github.com/LuckyKuang/sub2api-plus/internal/server/middleware"
	"github.com/LuckyKuang/sub2api-plus/internal/service"
	"github.com/alicebob/miniredis/v2"
	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

type openAIAccountInflightFixture struct {
	handler             *OpenAIGatewayHandler
	key                 *service.APIKey
	accounts            []*service.Account
	rdb                 *redis.Client
	billingCache        service.BillingCache
	concurrency         service.ConcurrencyCache
	counts              *handlerAccountWaitPlanCache
	upstream            *handlerAccountWaitPlanUpstream
	foreignReservations map[string]string
}

func newOpenAIAccountInflightFixture(t *testing.T, model string) *openAIAccountInflightFixture {
	t.Helper()
	const userID, groupID = int64(89701), int64(89702)
	const balance = .003
	ctx := context.Background()
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })
	billingCache := repository.NewBillingCache(rdb)
	require.NoError(t, billingCache.SetUserBalance(ctx, userID, balance))
	raw := repository.NewConcurrencyCache(rdb, 15, 900)
	owned, err := raw.AcquireUserSlot(ctx, userID, 3, "foreign-user-owner")
	require.NoError(t, err)
	require.True(t, owned)
	group := &service.Group{ID: groupID, Hydrated: true, Platform: service.PlatformOpenAI, Status: service.StatusActive, RateMultiplier: 1,
		AllowMessagesDispatch: true}
	var accounts []*service.Account
	var accountValues []service.Account
	for _, accountID := range []int64{89703, 89704} {
		owned, err := raw.AcquireAccountSlot(ctx, accountID, 2, "foreign-account-owner")
		require.NoError(t, err)
		require.True(t, owned)
		account := &service.Account{
			ID: accountID, Platform: service.PlatformOpenAI, Type: service.AccountTypeAPIKey,
			Status: service.StatusActive, Schedulable: true, Concurrency: 2,
			GroupIDs: []int64{groupID}, AccountGroups: []service.AccountGroup{{AccountID: accountID, GroupID: groupID}},
			Credentials: map[string]any{"api_key": "openai-inflight-fixture", "base_url": "https://openai-inflight.invalid",
				"model_mapping": map[string]any{model: "gpt-6-sol"}},
			Extra: map[string]any{"openai_responses_supported": true},
		}
		accounts = append(accounts, account)
		accountValues = append(accountValues, *account)
	}
	cfg := &config.Config{}
	cfg.Default.RateMultiplier = 1
	cfg.Gateway.MaxBodySize = 1 << 20
	cfg.Gateway.MaxAccountSwitches = 2
	cfg.Billing.InflightReservation = config.InflightReservationConfig{Enabled: true, TTLSeconds: 60, DefaultMaxTokens: 1000}
	billing := service.NewBillingCacheService(billingCache, nil, nil, nil, nil, nil, cfg, nil)
	t.Cleanup(billing.Stop)
	key := &service.APIKey{ID: 89705, UserID: userID, GroupID: &group.ID, Group: group, Status: service.StatusActive,
		User: &service.User{ID: userID, Status: service.StatusActive, Concurrency: 3, Balance: balance}}
	foreignHold, err := billing.ReserveInflight(ctx, key.User, group, nil, .0001)
	require.NoError(t, err)
	require.NotNil(t, foreignHold)
	t.Cleanup(foreignHold.HandlerDone)
	foreignReservations, err := rdb.HGetAll(ctx, "billing:inflight_amt:{89701}").Result()
	require.NoError(t, err)
	require.Len(t, foreignReservations, 1)
	counts := &handlerAccountWaitPlanCache{ConcurrencyCache: raw}
	concurrency := service.NewConcurrencyService(counts)
	upstream := &handlerAccountWaitPlanUpstream{}
	snapshot := service.NewSchedulerSnapshotService(&fakeSchedulerCache{accounts: accounts}, nil, nil, nil, nil)
	prices := service.NewBillingService(cfg, nil)
	accountRepo := &openAIWSFailoverHandlerAccountRepoStub{accounts: accountValues}
	gateway := service.NewOpenAIGatewayService(accountRepo, nil, nil, nil, nil, nil, nil, cfg, snapshot, concurrency,
		prices, nil, billing, upstream, &service.DeferredService{}, nil, nil, service.NewModelPricingResolver(nil, prices), nil, nil, nil, nil, nil)
	h := NewOpenAIGatewayHandler(gateway, concurrency, billing, &service.APIKeyService{}, nil, nil, nil, nil, cfg)
	return &openAIAccountInflightFixture{handler: h, key: key, accounts: accounts, rdb: rdb, billingCache: billingCache,
		concurrency: raw, counts: counts, upstream: upstream, foreignReservations: foreignReservations}
}

func (f *openAIAccountInflightFixture) assertOnlyForeignOwnersRemain(t *testing.T) {
	t.Helper()
	ctx := context.Background()
	for _, account := range f.accounts {
		owners, err := f.concurrency.GetAccountConcurrency(ctx, account.ID)
		require.NoError(t, err)
		require.Equal(t, 1, owners, "return only this request's account owner")
		waiting, err := f.concurrency.GetAccountWaitingCount(ctx, account.ID)
		require.NoError(t, err)
		require.Zero(t, waiting, "a rejected request cannot leave a queued account waiter")
	}
	users, err := f.concurrency.GetUserConcurrency(ctx, f.key.UserID)
	require.NoError(t, err)
	require.Equal(t, 1, users, "preserve the other request's global user owner")
	remaining, err := f.rdb.HGetAll(ctx, "billing:inflight_amt:{89701}").Result()
	require.NoError(t, err)
	require.Equal(t, f.foreignReservations, remaining, "return this request's monetary hold and preserve the existing hold")
	currentBalance, err := f.billingCache.GetUserBalance(ctx, f.key.UserID)
	require.NoError(t, err)
	require.Equal(t, .003, currentBalance, "rejection cannot charge usage")
	require.Zero(t, f.upstream.calls.Load(), "insufficient mapped-model balance must reject before forwarding")
}

// These real HTTP handlers use the default channel_mapped billing source and
// no public custom card. Each Luna estimate fits $0.003 less the foreign hold;
// the selected Sol model's 1000 output tokens alone cost $0.01 and cannot fit.
// Checking one acquired account proves the initial estimate passed and the
// selected-account admission rejected without retrying another owner.
func TestOpenAIHTTPHandlersRejectUnfundedMappedLunaBeforeForwarding(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, model := range []string{"gpt-6-luna", "gpt-5.6-luna"} {
		for _, endpoint := range []string{"responses", "chat/completions", "messages"} {
			for _, stream := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/%s/stream=%t", model, endpoint, stream), func(t *testing.T) {
					f := newOpenAIAccountInflightFixture(t, model)
					body := fmt.Sprintf(`{"model":%q,"max_output_tokens":1000,"stream":%t,"input":"hi"}`, model, stream)
					handle := f.handler.Responses
					if endpoint == "chat/completions" {
						body = fmt.Sprintf(`{"model":%q,"max_completion_tokens":1000,"stream":%t,"messages":[{"role":"user","content":"hi"}]}`, model, stream)
						handle = f.handler.ChatCompletions
					} else if endpoint == "messages" {
						body = fmt.Sprintf(`{"model":%q,"max_tokens":1000,"stream":%t,"messages":[{"role":"user","content":"hi"}]}`, model, stream)
						handle = f.handler.Messages
					}
					router := gin.New()
					router.Use(func(c *gin.Context) {
						c.Request = c.Request.WithContext(context.WithValue(c.Request.Context(), ctxkey.Group, f.key.Group))
						c.Set(string(middleware.ContextKeyAPIKey), f.key)
						c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: f.key.UserID, Concurrency: 3})
						c.Next()
					})
					path := "/v1/" + endpoint
					router.POST(path, handle)
					recorder := httptest.NewRecorder()
					request := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
					request.Header.Set("Content-Type", "application/json")
					router.ServeHTTP(recorder, request)
					require.Equal(t, http.StatusForbidden, recorder.Code, recorder.Body.String())
					require.Contains(t, recorder.Body.String(), "billing_error", "preserve each protocol's pre-stream billing error")
					require.Equal(t, int32(1), f.counts.accountAcquisitions.Load(), "reject at selected-account admission without failover")
					require.Equal(t, int32(1), f.counts.userAcquisitions.Load())
					require.Equal(t, int32(1), f.counts.userReleases.Load())
					require.Zero(t, f.counts.queueAttempts.Load(), "a billing rejection cannot enter an account wait queue")
					f.assertOnlyForeignOwnersRemain(t)
				})
			}
		}
	}
}

// Both slot-admission branches own a different release path. Even when a
// scheduler supplies a WaitPlan whose slot becomes available immediately, the
// mapped-model balance check must happen after acquiring and before binding.
func TestOpenAIAccountSlotRejectsMappedPriceAfterAcquiredAndWaitPlan(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, acquired := range []bool{false, true} {
		t.Run(fmt.Sprintf("already_acquired=%t", acquired), func(t *testing.T) {
			f := newOpenAIAccountInflightFixture(t, "gpt-6-luna")
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			body := []byte(`{"model":"gpt-6-luna","input":"hi","max_output_tokens":1000}`)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(string(body)))
			ctx := context.WithValue(c.Request.Context(), ctxkey.Group, f.key.Group)
			// The helper starts after the HTTP audit/snapshot stage, exercised by
			// the complete handlers above; provide that actual service snapshot.
			ctx = f.handler.gatewayService.WithBillingPricingSnapshot(ctx, f.key, time.Now(), "gpt-6-luna")
			c.Request = c.Request.WithContext(ctx)
			done, err := reserveInflightBalance(c, f.handler.billingCacheService, f.handler.gatewayService, f.key, nil,
				tokenInflightEstimate(service.ContentModerationProtocolOpenAIResponses, "gpt-6-luna", body))
			require.NoError(t, err, "the initial Luna estimate fits available balance")
			t.Cleanup(done)
			require.Greater(t, service.InflightReservationFromContext(c.Request.Context()).Amount(), 0.0)
			account := f.accounts[0]
			selection := &service.AccountSelectionResult{Account: account, Acquired: acquired,
				WaitPlan: &service.AccountWaitPlan{AccountID: account.ID, MaxConcurrency: 2, MaxWaiting: 2, Timeout: time.Second}}
			releases := 0
			if acquired {
				owned, err := f.concurrency.AcquireAccountSlot(ctx, account.ID, 2, "selected-owner")
				require.NoError(t, err)
				require.True(t, owned)
				selection.ReleaseFunc = func() {
					releases++
					require.NoError(t, f.concurrency.ReleaseAccountSlot(context.Background(), account.ID, "selected-owner"))
				}
			}
			var started bool
			release, result := f.handler.acquireOpenAIAccountSlot(c, f.key.GroupID, "", selection, true, &started, zap.NewNop(), nil)
			require.Equal(t, openAISlotAcquireFailed, result, recorder.Body.String())
			require.Nil(t, release, "failed admission cannot hand an account owner to the forwarder")
			require.Equal(t, http.StatusForbidden, recorder.Code, recorder.Body.String())
			require.Contains(t, recorder.Body.String(), "billing_error")
			require.False(t, started, "a selected-account billing rejection must precede stream output")
			if acquired {
				require.Equal(t, 1, releases, "return the scheduler's already acquired owner exactly once")
			} else {
				require.Equal(t, int32(1), f.counts.accountAcquisitions.Load(), "WaitPlan must first acquire its own slot")
			}
			done()
			f.assertOnlyForeignOwnersRemain(t)
		})
	}
}
