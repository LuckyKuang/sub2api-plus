//go:build integration

package integration

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/config"
	"github.com/LuckyKuang/sub2api-plus/internal/domain"
	"github.com/LuckyKuang/sub2api-plus/internal/repository"
	"github.com/LuckyKuang/sub2api-plus/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

// Scheduler Lua scripts also receive key prefixes in ARGV. Namespace those
// declarations as well as ordinary KEYS without replacing real Redis behavior.
type sevenManagementRedisNamespace struct{ prefix string }

func (h sevenManagementRedisNamespace) DialHook(next redis.DialHook) redis.DialHook { return next }

func (h sevenManagementRedisNamespace) prefixKeys(command redis.Cmder) {
	sevenBusinessRedisNamespace{prefix: h.prefix}.prefixKeys(command)
	for index, argument := range command.Args() {
		if value, ok := argument.(string); ok && strings.HasPrefix(value, "sched:") {
			command.Args()[index] = h.prefix + value
		}
	}
}

func (h sevenManagementRedisNamespace) ProcessHook(next redis.ProcessHook) redis.ProcessHook {
	return func(ctx context.Context, command redis.Cmder) error {
		h.prefixKeys(command)
		return next(ctx, command)
	}
}

func (h sevenManagementRedisNamespace) ProcessPipelineHook(next redis.ProcessPipelineHook) redis.ProcessPipelineHook {
	return func(ctx context.Context, commands []redis.Cmder) error {
		for _, command := range commands {
			h.prefixKeys(command)
		}
		return next(ctx, commands)
	}
}

func sevenManagementProviders() []sevenBusinessProvider {
	return []sevenBusinessProvider{
		{"openai", "codex", "0.165.0", "codex_cli_rs/0.165.0 (Ubuntu 24.04; x86_64) xterm-256color", nil},
		{"grok", "grok", "1.0.45", "grok-shell/1.0.45 (linux; x86_64)", nil},
		{"deepseek", "deepseek", "0.2.0-rc.2", "deepseek-harness/0.2.0-rc.2 (+https://github.com/deepseek-ai/deepseek-harness)", nil},
		{"kimi", "kimi", "2.1.1", "kimi-code-cli/2.1.1", nil},
		{"minimax", "minimax_apikey", "0.91.1", "Anthropic/JS 0.91.1", nil},
		{"stepfun", "stepfun", "", "stepcode (linux 6.8.0-31-generic; x64)", nil},
		{"zhipu", "zcode", "3.16.2", "ZCode/3.16.2 ai/6.0.193 ai-sdk/provider-utils/4.0.39 runtime/node.js/22", nil},
	}
}

// OUTBOUND_IDENTITY.md requires every new operation to resolve its current
// credential owner and saved route. A management write on instance A therefore
// has to invalidate instance B's warmed Redis account AND scheduler snapshots.
// This test uses production PostgreSQL repositories, the production outbox
// worker, two independent Redis clients/caches, the production selector and
// forwarding service, and real HTTP destinations/proxies. No test code copies
// edited database rows into the second instance's cache.
func TestSevenProviderManagementEditsReachOtherInstanceBeforeForwarding(t *testing.T) {
	gin.SetMode(gin.TestMode)
	services := newSevenBusinessServices(t)
	for _, provider := range sevenManagementProviders() {
		t.Run(provider.platform, func(t *testing.T) {
			runSevenManagementLifecycle(t, services, provider)
		})
	}
}

func runSevenManagementLifecycle(t *testing.T, services *sevenBusinessServices, provider sevenBusinessProvider) {
	t.Helper()
	ctx := context.Background()
	prefix := "seven-management:" + uuid.NewString() + ":"
	t.Cleanup(func() { services.cleanRedisNamespace(t, prefix) })
	newRedis := func() *redis.Client {
		options := *services.redis.Options()
		client := redis.NewClient(&options)
		client.AddHook(sevenManagementRedisNamespace{prefix: prefix})
		t.Cleanup(func() { require.NoError(t, client.Close()) })
		return client
	}
	redisA, redisB := newRedis(), newRedis()
	accountRepoA := repository.NewAccountRepository(services.ent, services.db, nil)
	accountRepoB := repository.NewAccountRepository(services.ent, services.db, nil)
	proxyRepo := repository.NewProxyRepository(services.ent, services.db)
	proxyManager := service.NewProxyService(proxyRepo)
	proxyA, proxyB := newSevenBusinessProxy(t, http.StatusOK), newSevenBusinessProxy(t, http.StatusOK)
	savedProxy := proxyA.savedProxy(t, proxyRepo, "management-original")
	proxyIDs := []int64{savedProxy.ID}
	t.Cleanup(func() {
		for _, id := range proxyIDs {
			_, err := services.db.ExecContext(ctx, "DELETE FROM proxies WHERE id=$1", id)
			require.NoError(t, err)
		}
	})
	var directTCP atomic.Int64
	destination := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		// An explicit management unbinding authorizes exactly one direct request.
		require.Equal(t, "Bearer management-rotated-key", request.Header.Get("Authorization"))
		require.Empty(t, request.Header.Get("Proxy-Authorization"))
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"management-direct","object":"chat.completion","model":"business-chat","choices":[{"index":0,"message":{"role":"assistant","content":"business success"},"finish_reason":"stop"}],"usage":{"prompt_tokens":10,"completion_tokens":5,"total_tokens":15}}`)
	}))
	destination.Config.ConnState = func(_ net.Conn, state http.ConnState) {
		if state == http.StateNew {
			directTCP.Add(1)
		}
	}
	destination.Start()
	t.Cleanup(destination.Close)
	account := &service.Account{Name: "seven-management-" + uuid.NewString(), Platform: provider.platform, Type: service.AccountTypeAPIKey,
		Status: service.StatusActive, Schedulable: true, Concurrency: 2, Priority: -100, ProxyID: &savedProxy.ID,
		Credentials: sevenBusinessCredentials(provider, "management-original", destination.URL),
		Extra:       map[string]any{"openai_responses_supported": false, "management_marker": "retain"}}
	require.NoError(t, accountRepoA.Create(ctx, account))
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
	worker := service.NewSchedulerSnapshotService(cacheA, outbox, accountRepoA, nil, cfg)
	reader := service.NewSchedulerSnapshotService(cacheB, nil, accountRepoB, nil, cfg)
	worker.Start()
	t.Cleanup(worker.Stop)
	// Wait for the startup writer's last bucket, then for all pre-existing
	// events. The later edit cannot be accidentally refreshed by startup work.
	platforms := domain.CompositePrecedencePlatformIDs()
	last := service.SchedulerBucket{Platform: platforms[len(platforms)-1], Mode: service.SchedulerModeForced}
	require.Eventually(t, func() bool {
		// Empty buckets deliberately return a GetSnapshot cache miss even once
		// rebuilt. The ready marker is the production completion boundary.
		ready, err := redisA.Get(ctx, "sched:ready:"+last.String()).Result()
		return err == nil && ready == "1"
	}, 10*time.Second, 20*time.Millisecond, "initial scheduler rebuild must finish before management edits")
	waitOutbox := func() {
		maxID, err := outbox.MaxID(ctx)
		require.NoError(t, err)
		require.Eventually(t, func() bool {
			watermark, err := cacheB.GetOutboxWatermark(ctx)
			return err == nil && watermark >= maxID
		}, 8*time.Second, 20*time.Millisecond, "the other instance must observe the production outbox consumer watermark")
	}
	waitOutbox()
	concurrency := service.NewConcurrencyService(repository.NewConcurrencyCache(redisB, 15, 30))
	gateway := service.NewOpenAIGatewayService(accountRepoB, nil, nil, nil, nil, nil, repository.NewGatewayCache(redisB), cfg, reader,
		concurrency, nil, nil, nil, repository.NewHTTPUpstream(cfg), nil, nil, nil, nil, nil, nil, nil, nil, repository.ProvideOpenAISessionRouteStore(services.db, redisB))
	forward := func(expectedProxy, expectedBearer string) {
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
		route, err := selection.Account.StrictProxyURL()
		require.NoError(t, err)
		require.Equal(t, expectedProxy, route)
		require.Equal(t, expectedBearer, selection.Account.GetCredential("api_key"))
		body := []byte(`{"model":"business-chat","messages":[{"role":"user","content":"management continuation"}]}`)
		recorder := httptest.NewRecorder()
		inbound, _ := gin.CreateTestContext(recorder)
		inbound.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(body)).WithContext(requestCtx)
		inbound.Request.Header.Set("User-Agent", "untrusted-management-browser/99")
		result, err := gateway.ForwardAsChatCompletions(requestCtx, inbound, selection.Account, body, "", "")
		require.NoError(t, err)
		require.NotNil(t, result)
		require.Contains(t, recorder.Body.String(), "business success")
	}
	readOwner := func() *service.Account {
		cached, err := reader.GetAccount(ctx, account.ID)
		require.NoError(t, err)
		require.NotNil(t, cached)
		return cached
	}
	forward(savedProxy.URL(), "business-key-management-original")
	oldOwner := readOwner()
	maxID, err := outbox.MaxID(ctx)
	require.NoError(t, err)
	endpointB, err := url.Parse(proxyB.server.URL)
	require.NoError(t, err)
	portB, err := strconv.Atoi(endpointB.Port())
	require.NoError(t, err)
	hostB, usernameB, passwordB := endpointB.Hostname(), "management-proxy-new-user", "management-proxy-new-password"
	edited, err := proxyManager.Update(ctx, savedProxy.ID, service.UpdateProxyRequest{Host: &hostB, Port: &portB, Username: &usernameB, Password: &passwordB})
	require.NoError(t, err)
	var events int
	require.NoError(t, services.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM scheduler_outbox WHERE id>$1 AND (account_id=$2 OR payload->'account_ids' @> jsonb_build_array($2::bigint))`, maxID, account.ID).Scan(&events))
	require.Positive(t, events, "editing a bound proxy must atomically enqueue every affected provider account, including accounts without a probe snapshot")
	waitOutbox()
	forward(edited.URL(), "business-key-management-original")
	require.Zero(t, directTCP.Load(), "a management route edit cannot authorize direct egress")
	require.Len(t, proxyA.snapshot(), 1, "the warmed instance must stop sending to the old route")
	require.Len(t, proxyB.snapshot(), 1)
	require.Equal(t, "Basic "+base64.StdEncoding.EncodeToString([]byte(usernameB+":"+passwordB)), proxyB.snapshot()[0].headers.Get("Proxy-Authorization"))
	renamed := "management-renamed-" + uuid.NewString()
	_, err = proxyManager.Update(ctx, savedProxy.ID, service.UpdateProxyRequest{Name: &renamed})
	require.NoError(t, err)
	waitOutbox()
	require.Equal(t, renamed, readOwner().Proxy.Name, "non-transport proxy metadata must also reach the other instance's cached account")

	// An old observation cannot use the shared SQL owner-CAS port after the
	// proxy row changed in place. Its failure must not publish cache or Extra.
	controlWriter, ok := accountRepoA.(service.CNControlSnapshotRepository)
	require.True(t, ok)
	applied, err := controlWriter.UpdateCNControlExtraIfUnchanged(ctx, oldOwner, map[string]any{"management_stale_observation": true})
	require.NoError(t, err)
	require.False(t, applied)
	require.NotContains(t, readOwner().Extra, "management_stale_observation")

	account, err = accountRepoA.GetByID(ctx, account.ID)
	require.NoError(t, err)
	account.Credentials["api_key"] = "management-rotated-key"
	require.NoError(t, accountRepoA.Update(ctx, account))
	waitOutbox()
	forward(edited.URL(), "management-rotated-key")

	account.Schedulable = false
	require.NoError(t, accountRepoA.Update(ctx, account))
	waitOutbox()
	listed, _, err := reader.ListSchedulableAccounts(ctx, nil, provider.platform, true)
	require.NoError(t, err)
	for _, candidate := range listed {
		require.NotEqual(t, account.ID, candidate.ID, "a management pause must evict the account from the other instance's warmed scheduler")
	}
	require.False(t, readOwner().Schedulable)
	require.Len(t, proxyB.snapshot(), 2, "pausing must not send a provider request")

	account.Schedulable, account.ProxyID = true, nil
	require.NoError(t, accountRepoA.Update(ctx, account))
	waitOutbox()
	forward("", "management-rotated-key")
	require.Equal(t, int64(1), directTCP.Load())
	// Delete the now-unbound route and create a distinct row before rebinding.
	// A cached proxy object cannot resurrect the deleted row or its auth.
	require.NoError(t, proxyManager.Delete(ctx, savedProxy.ID))
	_, err = proxyRepo.GetByID(ctx, savedProxy.ID)
	require.ErrorIs(t, err, service.ErrProxyNotFound)
	rebound, err := proxyManager.Create(ctx, service.CreateProxyRequest{Name: "management-rebound-" + uuid.NewString(), Protocol: "http",
		Host: hostB, Port: portB, Username: "management-rebound-user", Password: "management-rebound-password"})
	require.NoError(t, err)
	proxyIDs = append(proxyIDs, rebound.ID)
	require.NotEqual(t, savedProxy.ID, rebound.ID)
	account.ProxyID = &rebound.ID
	require.NoError(t, accountRepoA.Update(ctx, account))
	waitOutbox()
	forward(rebound.URL(), "management-rotated-key")
	require.Equal(t, int64(1), directTCP.Load(), "rebinding must revoke permission for later direct requests")
	require.Len(t, proxyA.snapshot(), 1)
	require.Len(t, proxyB.snapshot(), 3)
	require.Equal(t, "Bearer business-key-management-original", proxyA.snapshot()[0].headers.Get("Authorization"))
	require.Equal(t, provider.userAgent, proxyA.snapshot()[0].headers.Get("User-Agent"))
	expectedBearers := []string{"Bearer business-key-management-original", "Bearer management-rotated-key", "Bearer management-rotated-key"}
	for index, request := range proxyB.snapshot() {
		require.Equal(t, expectedBearers[index], request.headers.Get("Authorization"), "actual outbound credentials must follow the saved owner at each management stage")
		require.Equal(t, provider.userAgent, request.headers.Get("User-Agent"))
		require.NotContains(t, fmt.Sprint(request.headers), "untrusted-management-browser")
	}
	require.Equal(t, "Bearer management-rotated-key", proxyB.snapshot()[2].headers.Get("Authorization"))
	require.Equal(t, "Basic "+base64.StdEncoding.EncodeToString([]byte("management-rebound-user:management-rebound-password")), proxyB.snapshot()[2].headers.Get("Proxy-Authorization"))
	require.Equal(t, "retain", readOwner().Extra["management_marker"])
}

// Proxy edits and their cache-invalidation event form one PostgreSQL commit.
// A failed event must leave the original usable route intact rather than
// silently committing a route that other instances can never observe.
func TestSevenProviderManagementProxyEditOutboxFailureRollsBackRoute(t *testing.T) {
	services := newSevenBusinessServices(t)
	ctx := context.Background()
	proxyRepo := repository.NewProxyRepository(services.ent, services.db)
	accountRepo := repository.NewAccountRepository(services.ent, services.db, nil)
	proxy := &service.Proxy{Name: "management-atomic-" + uuid.NewString(), Protocol: "http", Host: "original.example", Port: 8080,
		Username: "original-user", Password: "original-password", Status: service.StatusActive}
	require.NoError(t, proxyRepo.Create(ctx, proxy))
	t.Cleanup(func() {
		_, err := services.db.ExecContext(ctx, "DELETE FROM proxies WHERE id=$1", proxy.ID)
		require.NoError(t, err)
	})
	account := &service.Account{Name: "management-atomic-" + uuid.NewString(), Platform: service.PlatformKimi, Type: service.AccountTypeAPIKey,
		Status: service.StatusActive, Schedulable: true, Concurrency: 1, ProxyID: &proxy.ID,
		Credentials: map[string]any{"api_key": "atomic-owner"}, Extra: map[string]any{"management_marker": "retain"}}
	require.NoError(t, accountRepo.Create(ctx, account))
	t.Cleanup(func() {
		_, err := services.db.ExecContext(ctx, "DELETE FROM scheduler_outbox WHERE account_id=$1 OR payload->'account_ids' @> jsonb_build_array($1::bigint)", account.ID)
		require.NoError(t, err)
		_, err = services.db.ExecContext(ctx, "DELETE FROM accounts WHERE id=$1", account.ID)
		require.NoError(t, err)
	})
	owner, err := accountRepo.GetByID(ctx, account.ID)
	require.NoError(t, err)
	function := "management_outbox_fault_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	_, err = services.db.ExecContext(ctx, fmt.Sprintf(`CREATE FUNCTION %s() RETURNS trigger LANGUAGE plpgsql AS $fault$
		BEGIN
			IF NEW.payload->'account_ids' @> '[%d]'::jsonb THEN
				RAISE EXCEPTION 'management outbox storage fault';
			END IF;
			RETURN NEW;
		END $fault$`, function, account.ID))
	require.NoError(t, err)
	t.Cleanup(func() {
		_, err := services.db.ExecContext(ctx, "DROP FUNCTION "+function+"() CASCADE")
		require.NoError(t, err)
	})
	_, err = services.db.ExecContext(ctx, "CREATE TRIGGER "+function+" BEFORE INSERT ON scheduler_outbox FOR EACH ROW EXECUTE FUNCTION "+function+"()")
	require.NoError(t, err)
	proxy.Host, proxy.Port, proxy.Password = "replacement.example", 9090, "replacement-password"
	err = proxyRepo.Update(ctx, proxy)
	require.ErrorContains(t, err, "management outbox storage fault")
	persisted, err := accountRepo.GetByID(ctx, account.ID)
	require.NoError(t, err)
	require.Equal(t, owner.Proxy, persisted.Proxy, "failed invalidation must roll back the proxy update")
	require.Equal(t, owner.Credentials, persisted.Credentials)
	require.Equal(t, owner.Extra, persisted.Extra)
}
