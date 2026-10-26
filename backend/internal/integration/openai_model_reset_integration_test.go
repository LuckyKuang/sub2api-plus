//go:build integration

package integration

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strconv"
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

func TestOpenAIAPIKeyAdminResetRecoversObservedModelTransient(t *testing.T) {
	services := newSevenBusinessServices(t)
	for _, where := range []string{"same-process", "other-process"} {
		t.Run(where, func(t *testing.T) {
			ctx := context.Background()
			prefix := "openai-model-recovery:" + uuid.NewString() + ":"
			t.Cleanup(func() { services.cleanRedisNamespace(t, prefix) })
			options := *services.redis.Options()
			rdb := redis.NewClient(&options)
			rdb.AddHook(sevenManagementRedisNamespace{prefix: prefix})
			t.Cleanup(func() { require.NoError(t, rdb.Close()) })
			repo := repository.NewAccountRepository(services.ent, services.db, nil)
			var calls atomic.Int64
			var healthy atomic.Bool
			destination := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				require.Equal(t, "/v1/chat/completions", r.URL.Path)
				require.Equal(t, "Bearer business-key-openai-model-reset", r.Header.Get("Authorization"))
				w.Header().Set("Content-Type", "application/json")
				if healthy.Load() {
					_, _ = io.WriteString(w, `{"id":"recovered-openai","object":"chat.completion","model":"gpt-5.5","choices":[{"index":0,"message":{"role":"assistant","content":"recovered same owner"},"finish_reason":"stop"}],"usage":{"prompt_tokens":2,"completion_tokens":1,"total_tokens":3}}`)
					return
				}
				w.WriteHeader(http.StatusServiceUnavailable)
				_, _ = io.WriteString(w, `{"error":{"message":"isolated upstream temporary failure"}}`)
			}))
			t.Cleanup(destination.Close)
			provider := sevenBusinessProvider{platform: service.PlatformOpenAI, userAgent: service.DefaultOpenAICodexUserAgent}
			account := &service.Account{Name: "openai-reset-observed-model-" + uuid.NewString(), Platform: service.PlatformOpenAI, Type: service.AccountTypeAPIKey,
				Status: service.StatusActive, Schedulable: true, Concurrency: 2, Priority: -30000,
				Credentials: sevenBusinessCredentials(provider, "openai-model-reset", destination.URL), Extra: map[string]any{"openai_responses_supported": false}}
			account.Credentials["model_mapping"] = map[string]any{"gpt-5.5": "gpt-5.5"}
			require.NoError(t, repo.Create(ctx, account))
			t.Cleanup(func() {
				_, err := services.db.ExecContext(ctx, "DELETE FROM scheduler_outbox WHERE account_id=$1", account.ID)
				require.NoError(t, err)
				_, err = services.db.ExecContext(ctx, "DELETE FROM accounts WHERE id=$1", account.ID)
				require.NoError(t, err)
			})
			cfg := &config.Config{RunMode: config.RunModeSimple}
			cfg.Gateway.Scheduling.OutboxPollIntervalSeconds = 1
			cfg.Gateway.Scheduling.DbFallbackEnabled = true
			cfg.Security.URLAllowlist.AllowPrivateHosts = true
			cfg.Security.URLAllowlist.AllowInsecureHTTP = true
			cache := repository.NewSchedulerCache(rdb)
			outbox := repository.NewSchedulerOutboxRepository(services.db)
			worker := service.NewSchedulerSnapshotService(cache, outbox, repo, nil, cfg)
			worker.Start()
			t.Cleanup(worker.Stop)
			require.Eventually(t, func() bool {
				value, err := rdb.Get(ctx, "sched:ready:0:cline:forced").Result()
				return err == nil && value == "1"
			}, 10*time.Second, 20*time.Millisecond)
			waitOutbox := func() {
				maxID, err := outbox.MaxID(ctx)
				require.NoError(t, err)
				require.Eventually(t, func() bool { value, err := cache.GetOutboxWatermark(ctx); return err == nil && value >= maxID }, 8*time.Second, 20*time.Millisecond)
			}
			waitOutbox()
			concurrency := service.NewConcurrencyService(repository.NewConcurrencyCache(rdb, 15, 30))
			rateLimits := service.NewRateLimitService(repo, nil, cfg, nil, nil)
			gateway := service.NewOpenAIGatewayService(repo, nil, nil, nil, nil, nil, repository.NewGatewayCache(rdb), cfg, worker,
				concurrency, nil, rateLimits, nil, repository.NewHTTPUpstream(cfg), nil, nil, nil, nil, nil, nil, nil, nil, repository.ProvideOpenAISessionRouteStore(services.db, rdb))
			selectAccount := func() (*service.AccountSelectionResult, error) {
				selection, _, err := gateway.SelectAccountWithSchedulerForCapability(ctx, nil, "", "", "gpt-5.5", nil,
					service.OpenAIUpstreamTransportAny, service.OpenAIEndpointCapabilityChatCompletions, false, false, false, service.PlatformOpenAI)
				return selection, err
			}
			forward := func(selection *service.AccountSelectionResult) error {
				require.NotNil(t, selection)
				require.Equal(t, account.ID, selection.Account.ID)
				if selection.ReleaseFunc != nil {
					defer selection.ReleaseFunc()
				}
				body := []byte(`{"model":"gpt-5.5","messages":[{"role":"user","content":"recover observed failure"}],"stream":false}`)
				inbound, _ := gin.CreateTestContext(httptest.NewRecorder())
				inbound.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(body))
				_, err := gateway.ForwardAsChatCompletions(ctx, inbound, selection.Account, body, "", "")
				return err
			}
			for i := 0; i < 2; i++ {
				selection, err := selectAccount()
				require.NoError(t, err)
				require.Error(t, forward(selection))
			}
			selection, err := selectAccount()
			if selection != nil && selection.ReleaseFunc != nil {
				selection.ReleaseFunc()
			}
			require.Error(t, err, "two actual transient responses must activate the model circuit before reset")
			require.Nil(t, selection)
			require.Equal(t, int64(2), calls.Load())
			if where == "same-process" {
				adminRepo, ok := repo.(service.AdminAccountRepository)
				require.True(t, ok)
				admin := service.NewAdminService(cfg, nil, nil, adminRepo, nil, nil, nil, nil, nil, nil, nil, nil, nil, services.ent, nil, nil, nil, nil, gateway, nil, nil, nil, nil)
				_, err := admin.ClearAccountError(ctx, account.ID)
				require.NoError(t, err)
			} else {
				command := exec.Command(os.Args[0], "-test.run=^TestGrokAdminRecoveryIndependentProcess$", "-test.v", "-test.count=1")
				command.Env = append(os.Environ(), "SUB2API_TEST_GROK_RECOVERY_ACCOUNT_ID="+strconv.FormatInt(account.ID, 10), "SUB2API_TEST_GROK_RECOVERY_DSN="+services.dsn)
				output, err := command.CombinedOutput()
				require.NoError(t, err, "independent reset: %s", string(output))
				require.Contains(t, string(output), "--- PASS: TestGrokAdminRecoveryIndependentProcess")
			}
			waitOutbox()
			healthy.Store(true)
			selection, err = selectAccount()
			require.NoError(t, err, "the explicit administrator recovery must restore the actual failed model without waiting for the circuit TTL")
			require.NoError(t, forward(selection))
			require.Equal(t, int64(3), calls.Load())
			healthy.Store(false)
			for range 2 {
				selection, err = selectAccount()
				require.NoError(t, err, "a recovered generation must allow failures to establish its own streak")
				require.Error(t, forward(selection))
			}
			selection, err = selectAccount()
			if selection != nil && selection.ReleaseFunc != nil {
				selection.ReleaseFunc()
			}
			require.Error(t, err, "new actual failures must establish a fresh transient circuit after recovery")
			require.Nil(t, selection)
			require.Equal(t, int64(5), calls.Load())
			counts, err := concurrency.GetAccountConcurrencyBatch(ctx, []int64{account.ID})
			require.NoError(t, err)
			require.Zero(t, counts[account.ID], "real failure and recovery must release all Redis account slots")
		})
	}
}
