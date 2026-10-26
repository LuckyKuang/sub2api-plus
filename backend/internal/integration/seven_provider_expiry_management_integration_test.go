//go:build integration

package integration

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
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

// The product's expiry modes are explicit saved-binding operations:
// backup selects the configured healthy route, direct clears the binding, and
// none retains it. Existing ProxyExpirySuite and PROXY_LIFECYCLE.md define these
// outcomes independently of gateway forwarding. Exercise the complete persisted
// transition and its next real send for every supported provider.
func TestSevenProviderExpiredProxyFallbackAndRepairReachOtherInstance(t *testing.T) {
	gin.SetMode(gin.TestMode)
	services := newSevenBusinessServices(t)
	for _, provider := range sevenManagementProviders() {
		for _, mode := range []string{service.FallbackModeProxy, service.FallbackModeDirect, service.FallbackModeNone} {
			t.Run(provider.platform+"/"+mode, func(t *testing.T) {
				runSevenExpiryLifecycle(t, services, provider, mode)
			})
		}
	}
}

func runSevenExpiryLifecycle(t *testing.T, services *sevenBusinessServices, provider sevenBusinessProvider, mode string) {
	t.Helper()
	ctx := context.Background()
	prefix := "seven-expiry:" + uuid.NewString() + ":"
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
	original, backup := proxyA.savedProxy(t, proxyRepo, "expiry-original"), proxyB.savedProxy(t, proxyRepo, "expiry-backup")
	t.Cleanup(func() {
		for _, id := range []int64{original.ID, backup.ID} {
			_, err := services.db.ExecContext(ctx, "DELETE FROM proxies WHERE id=$1", id)
			require.NoError(t, err)
		}
	})
	var directSends atomic.Int64
	destination := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		directSends.Add(1)
		require.Equal(t, "Bearer business-key-expiry-owner", request.Header.Get("Authorization"))
		require.Equal(t, provider.userAgent, request.Header.Get("User-Agent"))
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"expiry-direct","object":"chat.completion","model":"business-chat","choices":[{"index":0,"message":{"role":"assistant","content":"business success"},"finish_reason":"stop"}],"usage":{"prompt_tokens":10,"completion_tokens":5,"total_tokens":15}}`)
	}))
	t.Cleanup(destination.Close)
	account := &service.Account{Name: "seven-expiry-" + uuid.NewString(), Platform: provider.platform, Type: service.AccountTypeAPIKey,
		Status: service.StatusActive, Schedulable: true, Concurrency: 2, Priority: -200, ProxyID: &original.ID,
		Credentials: sevenBusinessCredentials(provider, "expiry-owner", destination.URL), Extra: map[string]any{"openai_responses_supported": false}}
	require.NoError(t, accountRepo.Create(ctx, account))
	t.Cleanup(func() {
		_, err := services.db.ExecContext(ctx, "DELETE FROM scheduler_outbox WHERE account_id=$1 OR payload->'account_ids' @> jsonb_build_array($1::bigint)", account.ID)
		require.NoError(t, err)
		_, err = services.db.ExecContext(ctx, "DELETE FROM accounts WHERE id=$1", account.ID)
		require.NoError(t, err)
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
	forward := func(route string) {
		requestCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		selection, _, err := gateway.SelectAccountWithSchedulerForCapability(requestCtx, nil, "", "", "business-chat", nil,
			service.OpenAIUpstreamTransportAny, service.OpenAIEndpointCapabilityChatCompletions, false, false, false, provider.platform)
		require.NoError(t, err)
		require.NotNil(t, selection)
		if selection.ReleaseFunc != nil {
			defer selection.ReleaseFunc()
		}
		require.Equal(t, account.ID, selection.Account.ID)
		actualRoute, err := selection.Account.StrictProxyURL()
		require.NoError(t, err)
		require.Equal(t, route, actualRoute)
		body := []byte(`{"model":"business-chat","messages":[{"role":"user","content":"expiry continuation"}]}`)
		recorder := httptest.NewRecorder()
		inbound, _ := gin.CreateTestContext(recorder)
		inbound.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(body)).WithContext(requestCtx)
		result, err := gateway.ForwardAsChatCompletions(requestCtx, inbound, selection.Account, body, "", "")
		require.NoError(t, err)
		require.NotNil(t, result)
		require.Contains(t, recorder.Body.String(), "business success")
	}
	forward(original.URL())
	past := time.Now().Add(-time.Hour)
	original.ExpiresAt, original.FallbackMode = &past, mode
	if mode == service.FallbackModeProxy {
		original.BackupProxyID = &backup.ID
	}
	require.NoError(t, proxyRepo.Update(ctx, original))
	waitOutbox()
	expiredOwner, err := reader.GetAccount(ctx, account.ID)
	require.NoError(t, err)
	changed, err := proxyRepo.SweepExpiredProxies(ctx, time.Now())
	require.NoError(t, err)
	if mode == service.FallbackModeNone {
		require.Zero(t, changed)
	} else {
		require.EqualValues(t, 1, changed)
	}
	waitOutbox()
	persisted, err := accountRepo.GetByID(ctx, account.ID)
	require.NoError(t, err)
	current, err := reader.GetAccount(ctx, account.ID)
	require.NoError(t, err)
	switch mode {
	case service.FallbackModeDirect:
		require.Nil(t, persisted.ProxyID)
		require.Equal(t, &original.ID, persisted.ProxyFallbackOriginID)
		forward("")
	case service.FallbackModeProxy:
		require.Equal(t, &backup.ID, persisted.ProxyID)
		require.Equal(t, &original.ID, persisted.ProxyFallbackOriginID)
		forward(backup.URL())
	case service.FallbackModeNone:
		require.Equal(t, &original.ID, persisted.ProxyID)
		require.Nil(t, persisted.ProxyFallbackOriginID)
		require.Equal(t, service.StatusExpired, current.Proxy.Status, "even keep-binding expiry changes must refresh every provider's cached proxy status")
		forward(original.URL())
	}
	writer := accountRepo.(service.CNControlSnapshotRepository)
	applied, err := writer.UpdateCNControlExtraIfUnchanged(ctx, expiredOwner, map[string]any{"expiry_stale_observation": true})
	require.NoError(t, err)
	require.False(t, applied, "an operation started before expiry cannot publish after status or binding changes")
	changed, err = proxyRepo.SweepExpiredProxies(ctx, time.Now())
	require.NoError(t, err)
	require.Zero(t, changed, "repeated sweeps must not rebind an already handled account")
	waitOutbox()

	original.Status = service.StatusActive
	future := time.Now().Add(time.Hour)
	original.ExpiresAt = &future
	require.NoError(t, proxyRepo.Update(ctx, original))
	if mode != service.FallbackModeNone {
		require.NoError(t, accountRepo.RevertProxyFallback(ctx, account.ID))
	}
	waitOutbox()
	forward(original.URL())
	final, err := accountRepo.GetByID(ctx, account.ID)
	require.NoError(t, err)
	require.Equal(t, &original.ID, final.ProxyID)
	require.Nil(t, final.ProxyFallbackOriginID)
	require.Equal(t, service.StatusActive, final.Proxy.Status)
	require.NotContains(t, final.Extra, "expiry_stale_observation")
	expectedOriginal := 2
	if mode == service.FallbackModeNone {
		expectedOriginal = 3
	}
	require.Len(t, proxyA.snapshot(), expectedOriginal)
	if mode == service.FallbackModeProxy {
		require.Len(t, proxyB.snapshot(), 1)
	} else {
		require.Empty(t, proxyB.snapshot())
	}
	if mode == service.FallbackModeDirect {
		require.Equal(t, int64(1), directSends.Load())
	} else {
		require.Zero(t, directSends.Load())
	}
	for _, proxy := range []*sevenBusinessProxy{proxyA, proxyB} {
		for _, request := range proxy.snapshot() {
			require.Equal(t, "Bearer business-key-expiry-owner", request.headers.Get("Authorization"))
			require.Equal(t, provider.userAgent, request.headers.Get("User-Agent"))
		}
	}
}

func sevenExpiryStorageFixture(t *testing.T, services *sevenBusinessServices) (service.ProxyRepository, service.AccountRepository, *service.Proxy, *service.Account) {
	t.Helper()
	ctx := context.Background()
	proxyRepo := repository.NewProxyRepository(services.ent, services.db)
	accountRepo := repository.NewAccountRepository(services.ent, services.db, nil)
	past := time.Now().Add(-time.Hour)
	proxy := &service.Proxy{Name: "expiry-storage-" + uuid.NewString(), Protocol: "http", Host: "original.example", Port: 8080,
		Status: service.StatusActive, ExpiresAt: &past, FallbackMode: service.FallbackModeDirect}
	require.NoError(t, proxyRepo.Create(ctx, proxy))
	t.Cleanup(func() {
		_, err := services.db.ExecContext(ctx, "DELETE FROM proxies WHERE id=$1", proxy.ID)
		require.NoError(t, err)
	})
	account := &service.Account{Name: "expiry-storage-" + uuid.NewString(), Platform: service.PlatformKimi, Type: service.AccountTypeAPIKey,
		Status: service.StatusActive, Schedulable: true, Concurrency: 1, ProxyID: &proxy.ID, Credentials: map[string]any{"api_key": "expiry-owner"}}
	require.NoError(t, accountRepo.Create(ctx, account))
	t.Cleanup(func() {
		_, err := services.db.ExecContext(ctx, "DELETE FROM scheduler_outbox WHERE account_id=$1 OR payload->'account_ids' @> jsonb_build_array($1::bigint)", account.ID)
		require.NoError(t, err)
		_, err = services.db.ExecContext(ctx, "DELETE FROM accounts WHERE id=$1", account.ID)
		require.NoError(t, err)
	})
	return proxyRepo, accountRepo, proxy, account
}

func TestSevenProviderExpirySweepWaitsForRenewalAndRetainsRenewedBinding(t *testing.T) {
	services := newSevenBusinessServices(t)
	proxyRepo, accountRepo, proxy, account := sevenExpiryStorageFixture(t, services)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	tx, err := services.db.BeginTx(ctx, nil)
	require.NoError(t, err)
	defer func() { _ = tx.Rollback() }()
	_, err = tx.ExecContext(ctx, "UPDATE proxies SET expires_at=NOW()+INTERVAL '1 day',updated_at=NOW() WHERE id=$1", proxy.ID)
	require.NoError(t, err)
	type result struct {
		changed int64
		err     error
	}
	done := make(chan result, 1)
	go func() {
		changed, err := proxyRepo.SweepExpiredProxies(ctx, time.Now())
		done <- result{changed, err}
	}()
	require.Eventually(t, func() bool {
		var waiting bool
		err := services.db.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM pg_stat_activity WHERE datname=current_database() AND wait_event_type='Lock' AND query ILIKE '%proxies%')`).Scan(&waiting)
		return err == nil && waiting
	}, 2*time.Second, 10*time.Millisecond, "sweeper must wait for the concurrent management renewal")
	require.NoError(t, tx.Commit())
	got := <-done
	require.NoError(t, got.err)
	require.Zero(t, got.changed, "the old scan cannot expire a route renewed before the sweep obtains its row lock")
	current, err := accountRepo.GetByID(ctx, account.ID)
	require.NoError(t, err)
	require.Equal(t, &proxy.ID, current.ProxyID)
	require.Nil(t, current.ProxyFallbackOriginID)
	require.Equal(t, service.StatusActive, current.Proxy.Status)
	require.True(t, current.Proxy.ExpiresAt.After(time.Now()))
}

func TestSevenProviderExpiryAndRevertOutboxFailuresRollbackSavedBinding(t *testing.T) {
	services := newSevenBusinessServices(t)
	for _, operation := range []string{"expiry", "revert"} {
		t.Run(operation, func(t *testing.T) {
			ctx := context.Background()
			proxyRepo, accountRepo, proxy, account := sevenExpiryStorageFixture(t, services)
			if operation == "revert" {
				_, err := services.db.ExecContext(ctx, "UPDATE accounts SET proxy_id=NULL,proxy_fallback_origin_id=$2 WHERE id=$1", account.ID, proxy.ID)
				require.NoError(t, err)
			}
			owner, err := accountRepo.GetByID(ctx, account.ID)
			require.NoError(t, err)
			function := "expiry_outbox_fault_" + strings.ReplaceAll(uuid.NewString(), "-", "")
			_, err = services.db.ExecContext(ctx, fmt.Sprintf(`CREATE FUNCTION %s() RETURNS trigger LANGUAGE plpgsql AS $fault$
				BEGIN
					IF NEW.account_id=%d OR NEW.payload->'account_ids' @> '[%d]'::jsonb THEN
						RAISE EXCEPTION 'expiry outbox storage fault';
					END IF;
					RETURN NEW;
				END $fault$`, function, account.ID, account.ID))
			require.NoError(t, err)
			t.Cleanup(func() {
				_, err := services.db.ExecContext(ctx, "DROP FUNCTION "+function+"() CASCADE")
				require.NoError(t, err)
			})
			_, err = services.db.ExecContext(ctx, "CREATE TRIGGER "+function+" BEFORE INSERT ON scheduler_outbox FOR EACH ROW EXECUTE FUNCTION "+function+"()")
			require.NoError(t, err)
			if operation == "expiry" {
				_, err = proxyRepo.SweepExpiredProxies(ctx, time.Now())
			} else {
				err = accountRepo.RevertProxyFallback(ctx, account.ID)
			}
			require.ErrorContains(t, err, "expiry outbox storage fault")
			got, err := accountRepo.GetByID(ctx, account.ID)
			require.NoError(t, err)
			require.Equal(t, owner.ProxyID, got.ProxyID)
			require.Equal(t, owner.ProxyFallbackOriginID, got.ProxyFallbackOriginID)
			persistedProxy, err := proxyRepo.GetByID(ctx, proxy.ID)
			require.NoError(t, err)
			require.Equal(t, service.StatusActive, persistedProxy.Status)
		})
	}
}
