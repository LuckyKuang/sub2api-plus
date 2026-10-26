//go:build integration

package integration

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/config"
	"github.com/LuckyKuang/sub2api-plus/internal/repository"
	"github.com/LuckyKuang/sub2api-plus/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

// A deleted credential owner cannot survive in another node's warm scheduler
// or reappear when the administrator recreates the same display name. The new
// owner gets a distinct ID, credential and proxy, all observed at actual sends.
func TestSevenProviderAccountDeletionAndRecreationReachOtherInstance(t *testing.T) {
	gin.SetMode(gin.TestMode)
	services := newSevenBusinessServices(t)
	for _, provider := range sevenManagementProviders() {
		t.Run(provider.platform, func(t *testing.T) {
			ctx := context.Background()
			prefix := "seven-recreation:" + uuid.NewString() + ":"
			t.Cleanup(func() { services.cleanRedisNamespace(t, prefix) })
			newRedis := func() *redis.Client {
				options := *services.redis.Options()
				client := redis.NewClient(&options)
				client.AddHook(sevenManagementRedisNamespace{prefix: prefix})
				t.Cleanup(func() { require.NoError(t, client.Close()) })
				return client
			}
			redisA, redisB := newRedis(), newRedis()
			accountRepo := repository.NewAccountRepository(services.ent, services.db, nil)
			proxyRepo := repository.NewProxyRepository(services.ent, services.db)
			proxyA, proxyB := newSevenBusinessProxy(t, http.StatusOK), newSevenBusinessProxy(t, http.StatusOK)
			savedA, savedB := proxyA.savedProxy(t, proxyRepo, "deleted-owner"), proxyB.savedProxy(t, proxyRepo, "recreated-owner")
			t.Cleanup(func() {
				for _, id := range []int64{savedA.ID, savedB.ID} {
					_, err := services.db.ExecContext(ctx, "DELETE FROM proxies WHERE id=$1", id)
					require.NoError(t, err)
				}
			})
			var directSends atomic.Int64
			destination := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				directSends.Add(1)
				http.Error(w, "direct forwarding forbidden", http.StatusBadGateway)
			}))
			t.Cleanup(destination.Close)
			name := "seven-recreated-" + uuid.NewString()
			old := &service.Account{Name: name, Platform: provider.platform, Type: service.AccountTypeAPIKey,
				Status: service.StatusActive, Schedulable: true, Concurrency: 2, Priority: -300, ProxyID: &savedA.ID,
				Credentials: sevenBusinessCredentials(provider, "deleted-owner", destination.URL), Extra: map[string]any{"openai_responses_supported": false}}
			require.NoError(t, accountRepo.Create(ctx, old))
			accountIDs := []int64{old.ID}
			t.Cleanup(func() {
				for _, id := range accountIDs {
					_, err := services.db.ExecContext(ctx, "DELETE FROM scheduler_outbox WHERE account_id=$1", id)
					require.NoError(t, err)
					_, err = services.db.ExecContext(ctx, "DELETE FROM accounts WHERE id=$1", id)
					require.NoError(t, err)
				}
			})
			cfg := &config.Config{RunMode: config.RunModeSimple}
			cfg.Gateway.Scheduling.OutboxPollIntervalSeconds = 1
			cfg.Gateway.Scheduling.DbFallbackEnabled = true
			cfg.Security.URLAllowlist.AllowPrivateHosts = true
			cfg.Security.URLAllowlist.AllowInsecureHTTP = true
			cacheA, cacheB := repository.NewSchedulerCache(redisA), repository.NewSchedulerCache(redisB)
			outbox := repository.NewSchedulerOutboxRepository(services.db)
			worker := service.NewSchedulerSnapshotService(cacheA, outbox, accountRepo, nil, cfg)
			reader := service.NewSchedulerSnapshotService(cacheB, nil, repository.NewAccountRepository(services.ent, services.db, nil), nil, cfg)
			worker.Start()
			t.Cleanup(worker.Stop)
			require.Eventually(t, func() bool {
				ready, err := redisA.Get(ctx, "sched:ready:0:cline:forced").Result()
				return err == nil && ready == "1"
			}, 10*time.Second, 20*time.Millisecond)
			waitOutbox := func() {
				maxID, err := outbox.MaxID(ctx)
				require.NoError(t, err)
				require.Eventually(t, func() bool {
					watermark, err := cacheB.GetOutboxWatermark(ctx)
					return err == nil && watermark >= maxID
				}, 8*time.Second, 20*time.Millisecond)
			}
			waitOutbox()
			concurrency := service.NewConcurrencyService(repository.NewConcurrencyCache(redisB, 15, 30))
			gateway := service.NewOpenAIGatewayService(accountRepo, nil, nil, nil, nil, nil, repository.NewGatewayCache(redisB), cfg, reader,
				concurrency, nil, nil, nil, repository.NewHTTPUpstream(cfg), nil, nil, nil, nil, nil, nil, nil, nil, repository.ProvideOpenAISessionRouteStore(services.db, redisB))
			selectAccount := func() (*service.AccountSelectionResult, error) {
				selection, _, err := gateway.SelectAccountWithSchedulerForCapability(ctx, nil, "", "", "business-chat", nil,
					service.OpenAIUpstreamTransportAny, service.OpenAIEndpointCapabilityChatCompletions, false, false, false, provider.platform)
				return selection, err
			}
			forward := func(id int64) {
				selection, err := selectAccount()
				require.NoError(t, err)
				require.NotNil(t, selection)
				if selection.ReleaseFunc != nil {
					defer selection.ReleaseFunc()
				}
				require.Equal(t, id, selection.Account.ID)
				body := []byte(`{"model":"business-chat","messages":[{"role":"user","content":"account recreation"}]}`)
				recorder := httptest.NewRecorder()
				inbound, _ := gin.CreateTestContext(recorder)
				requestCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
				defer cancel()
				inbound.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(body)).WithContext(requestCtx)
				result, err := gateway.ForwardAsChatCompletions(requestCtx, inbound, selection.Account, body, "", "")
				require.NoError(t, err)
				require.NotNil(t, result)
				require.Contains(t, recorder.Body.String(), "business success")
			}
			forward(old.ID)
			require.NoError(t, accountRepo.Delete(ctx, old.ID))
			waitOutbox()
			cached, err := cacheB.GetAccount(ctx, old.ID)
			require.NoError(t, err)
			require.Nil(t, cached, "committed deletion must remove the other node's full credential snapshot")
			selection, err := selectAccount()
			if selection != nil && selection.ReleaseFunc != nil {
				selection.ReleaseFunc()
			}
			require.Error(t, err, "the deleted owner cannot remain available through a warmed scheduler")
			require.Nil(t, selection)
			require.Len(t, proxyA.snapshot(), 1)
			require.Empty(t, proxyB.snapshot())
			replacement := &service.Account{Name: name, Platform: provider.platform, Type: service.AccountTypeAPIKey,
				Status: service.StatusActive, Schedulable: true, Concurrency: 2, Priority: -300, ProxyID: &savedB.ID,
				Credentials: sevenBusinessCredentials(provider, "recreated-owner", destination.URL), Extra: map[string]any{"openai_responses_supported": false}}
			require.NoError(t, accountRepo.Create(ctx, replacement))
			accountIDs = append(accountIDs, replacement.ID)
			require.NotEqual(t, old.ID, replacement.ID)
			waitOutbox()
			forward(replacement.ID)
			require.Len(t, proxyA.snapshot(), 1, "recreating a display name cannot resurrect the old owner")
			require.Len(t, proxyB.snapshot(), 1)
			require.Equal(t, "Bearer business-key-deleted-owner", proxyA.snapshot()[0].headers.Get("Authorization"))
			require.Equal(t, "Bearer business-key-recreated-owner", proxyB.snapshot()[0].headers.Get("Authorization"))
			require.Equal(t, provider.userAgent, proxyB.snapshot()[0].headers.Get("User-Agent"))
			require.Zero(t, directSends.Load())
		})
	}
}
