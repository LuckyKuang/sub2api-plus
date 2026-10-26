//go:build integration

package integration

import (
	"bytes"
	"context"
	"database/sql"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	dbent "github.com/LuckyKuang/sub2api-plus/ent"
	"github.com/LuckyKuang/sub2api-plus/internal/config"
	"github.com/LuckyKuang/sub2api-plus/internal/repository"
	"github.com/LuckyKuang/sub2api-plus/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

// The reset runs in a separate process: sharing two gateway structs in one
// process would accidentally share Grok's package-level model limit store.
func TestGrokAdminResetReachesFailureHoldingProcessThroughOutbox(t *testing.T) {
	gin.SetMode(gin.TestMode)
	services := newSevenBusinessServices(t)
	for _, failure := range []string{"model-capacity", "model-free-usage", "account-unauthorized"} {
		t.Run(failure, func(t *testing.T) {
			ctx := context.Background()
			prefix := "grok-recovery-process:" + uuid.NewString() + ":"
			t.Cleanup(func() { services.cleanRedisNamespace(t, prefix) })
			newRedis := func() *redis.Client {
				options := *services.redis.Options()
				client := redis.NewClient(&options)
				client.AddHook(sevenManagementRedisNamespace{prefix: prefix})
				t.Cleanup(func() { require.NoError(t, client.Close()) })
				return client
			}
			redisA, redisReader := newRedis(), newRedis()
			repo := repository.NewAccountRepository(services.ent, services.db, nil)
			var calls atomic.Int64
			var healthy atomic.Bool
			destination := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				require.Equal(t, "/v1/chat/completions", r.URL.Path)
				require.Equal(t, "Bearer business-key-cross-instance-reset", r.Header.Get("Authorization"))
				w.Header().Set("Content-Type", "application/json")
				if healthy.Load() {
					_, _ = io.WriteString(w, `{"id":"recovered-grok","object":"chat.completion","model":"grok-4.5","choices":[{"index":0,"message":{"role":"assistant","content":"recovered same owner"},"finish_reason":"stop"}],"usage":{"prompt_tokens":2,"completion_tokens":1,"total_tokens":3}}`)
					return
				}
				status, body := http.StatusTooManyRequests, `{"error":{"message":"The model is currently at capacity due to high demand"}}`
				if failure == "model-free-usage" {
					status, body = http.StatusBadRequest, `{"error":{"code":"subscription:free-usage-exhausted","message":"You've used all the included free usage for model grok-4.5"}}`
				}
				if failure == "account-unauthorized" {
					status, body = http.StatusUnauthorized, `{"error":{"message":"isolated authentication failure"}}`
				}
				w.WriteHeader(status)
				_, _ = io.WriteString(w, body)
			}))
			t.Cleanup(destination.Close)
			provider := sevenBusinessProvider{platform: service.PlatformGrok, preset: "grok", version: "1.0.45", userAgent: "grok-shell/1.0.45 (linux; x86_64)"}
			account := &service.Account{Name: "cross-instance-grok-reset-" + uuid.NewString(), Platform: service.PlatformGrok, Type: service.AccountTypeAPIKey,
				Status: service.StatusActive, Schedulable: true, Concurrency: 2, Priority: -30000,
				Credentials: sevenBusinessCredentials(provider, "cross-instance-reset", destination.URL), Extra: map[string]any{"openai_responses_supported": false}}
			account.Credentials["model_mapping"] = map[string]any{"grok-4.5": "grok-4.5"}
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
			cacheA, cacheReader := repository.NewSchedulerCache(redisA), repository.NewSchedulerCache(redisReader)
			outbox := repository.NewSchedulerOutboxRepository(services.db)
			worker := service.NewSchedulerSnapshotService(cacheA, outbox, repo, nil, cfg)
			reader := service.NewSchedulerSnapshotService(cacheReader, nil, repository.NewAccountRepository(services.ent, services.db, nil), nil, cfg)
			worker.Start()
			t.Cleanup(worker.Stop)
			require.Eventually(t, func() bool {
				value, err := redisA.Get(ctx, "sched:ready:0:cline:forced").Result()
				return err == nil && value == "1"
			}, 10*time.Second, 20*time.Millisecond)
			waitOutbox := func() {
				maxID, err := outbox.MaxID(ctx)
				require.NoError(t, err)
				require.Eventually(t, func() bool {
					watermark, err := cacheReader.GetOutboxWatermark(ctx)
					return err == nil && watermark >= maxID
				}, 8*time.Second, 20*time.Millisecond, "the failure-holding process must observe the actual reset outbox watermark")
			}
			waitOutbox()
			concurrency := service.NewConcurrencyService(repository.NewConcurrencyCache(redisReader, 15, 30))
			gateway := service.NewOpenAIGatewayService(repo, nil, nil, nil, nil, nil, repository.NewGatewayCache(redisReader), cfg, reader,
				concurrency, nil, nil, nil, repository.NewHTTPUpstream(cfg), nil, nil, nil, nil, nil, nil, nil, nil, repository.ProvideOpenAISessionRouteStore(services.db, redisReader))
			selectAccount := func() (*service.AccountSelectionResult, error) {
				selection, _, err := gateway.SelectAccountWithSchedulerForCapability(ctx, nil, "", "", "grok-4.5", nil,
					service.OpenAIUpstreamTransportAny, service.OpenAIEndpointCapabilityChatCompletions, false, false, false, service.PlatformGrok)
				return selection, err
			}
			forward := func(selection *service.AccountSelectionResult) error {
				require.NotNil(t, selection)
				require.Equal(t, account.ID, selection.Account.ID)
				if selection.ReleaseFunc != nil {
					defer selection.ReleaseFunc()
				}
				body := []byte(`{"model":"grok-4.5","messages":[{"role":"user","content":"cross-instance reset"}],"stream":false}`)
				inbound, _ := gin.CreateTestContext(httptest.NewRecorder())
				inbound.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(body))
				_, err := gateway.ForwardAsChatCompletions(ctx, inbound, selection.Account, body, "", "")
				return err
			}
			selection, err := selectAccount()
			require.NoError(t, err)
			require.Error(t, forward(selection), "the real failure must install its own scheduling/model limit")
			require.Equal(t, int64(1), calls.Load(), "failure installation must follow the actual provider response rather than a local preparation error")
			waitOutbox()
			selection, err = selectAccount()
			if selection != nil && selection.ReleaseFunc != nil {
				selection.ReleaseFunc()
			}
			require.Error(t, err, "before reset the observed failure must still exclude this account/model")
			require.Nil(t, selection)
			require.Equal(t, int64(1), calls.Load())
			before, err := repo.GetByID(ctx, account.ID)
			require.NoError(t, err)
			command := exec.Command(os.Args[0], "-test.run=^TestGrokAdminRecoveryIndependentProcess$", "-test.v", "-test.count=1")
			command.Env = append(os.Environ(), "SUB2API_TEST_GROK_RECOVERY_ACCOUNT_ID="+strconv.FormatInt(account.ID, 10), "SUB2API_TEST_GROK_RECOVERY_DSN="+services.dsn)
			output, err := command.CombinedOutput()
			require.NoError(t, err, "independent admin reset process: %s", string(output))
			require.Contains(t, string(output), "--- PASS: TestGrokAdminRecoveryIndependentProcess")
			waitOutbox()
			after, err := reader.GetAccount(ctx, account.ID)
			require.NoError(t, err)
			require.Equal(t, before.Credentials, after.Credentials, "reset must recover the same credential owner")
			require.Nil(t, after.TempUnschedulableUntil)
			require.Nil(t, after.RateLimitResetAt)
			healthy.Store(true)
			selection, err = selectAccount()
			require.NoError(t, err, "a reset consumed through the production outbox must restore admission on the process that learned the failure")
			require.NoError(t, forward(selection))
			require.Equal(t, int64(2), calls.Load())
			healthy.Store(false)
			selection, err = selectAccount()
			require.NoError(t, err, "the recovered generation must admit its first new request")
			require.Error(t, forward(selection))
			waitOutbox()
			selection, err = selectAccount()
			if selection != nil && selection.ReleaseFunc != nil {
				selection.ReleaseFunc()
			}
			require.Error(t, err, "a real post-recovery failure must block the recovered generation again")
			require.Nil(t, selection)
			require.Equal(t, int64(3), calls.Load())
			counts, err := concurrency.GetAccountConcurrencyBatch(ctx, []int64{account.ID})
			require.NoError(t, err)
			require.Zero(t, counts[account.ID], "both failure and successful recovery must release their actual Redis account slots")
		})
	}
}

func TestGrokAdminRecoveryIndependentProcess(t *testing.T) {
	value := strings.TrimSpace(os.Getenv("SUB2API_TEST_GROK_RECOVERY_ACCOUNT_ID"))
	if value == "" {
		return
	}
	id, err := strconv.ParseInt(value, 10, 64)
	require.NoError(t, err)
	db, err := sql.Open("postgres", os.Getenv("SUB2API_TEST_GROK_RECOVERY_DSN"))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	client := dbent.NewClient(dbent.Driver(entsql.OpenDB(dialect.Postgres, db)))
	repo, ok := repository.NewAccountRepository(client, db, nil).(service.AdminAccountRepository)
	require.True(t, ok)
	admin := service.NewAdminService(&config.Config{}, nil, nil, repo, nil, nil, nil, nil, nil, nil, nil, nil, nil, client, nil, nil, nil, nil, &service.OpenAIGatewayService{}, nil, nil, nil, nil)
	updated, err := admin.ClearAccountError(context.Background(), id)
	require.NoError(t, err)
	require.Equal(t, id, updated.ID)
	require.Equal(t, service.StatusActive, updated.Status)
}
