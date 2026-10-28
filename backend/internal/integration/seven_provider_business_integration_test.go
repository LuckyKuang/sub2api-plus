//go:build integration

package integration

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	dbent "github.com/LuckyKuang/sub2api-plus/ent"
	_ "github.com/LuckyKuang/sub2api-plus/ent/runtime"
	"github.com/LuckyKuang/sub2api-plus/internal/config"
	"github.com/LuckyKuang/sub2api-plus/internal/handler"
	"github.com/LuckyKuang/sub2api-plus/internal/pkg/antigravity"
	"github.com/LuckyKuang/sub2api-plus/internal/pkg/outboundidentity"
	"github.com/LuckyKuang/sub2api-plus/internal/pkg/timezone"
	"github.com/LuckyKuang/sub2api-plus/internal/pkg/zcode"
	"github.com/LuckyKuang/sub2api-plus/internal/repository"
	middleware "github.com/LuckyKuang/sub2api-plus/internal/server/middleware"
	"github.com/LuckyKuang/sub2api-plus/internal/service"
	"github.com/LuckyKuang/sub2api-plus/internal/testutil/externalredis"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	_ "github.com/lib/pq"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	tcredis "github.com/testcontainers/testcontainers-go/modules/redis"
)

type sevenBusinessServices struct {
	db    *sql.DB
	ent   *dbent.Client
	redis *redis.Client
	dsn   string
}

// Keep the same external-service contract and pinned images as the repository
// integration harness. Docker CI can start its own services; Apple validation
// supplies disposable services through the maintained environment bindings.
func newSevenBusinessServices(t *testing.T) *sevenBusinessServices {
	t.Helper()
	ctx := context.Background()
	require.NoError(t, timezone.Init("UTC"))
	dsn := strings.TrimSpace(os.Getenv("SUB2API_TEST_POSTGRES_DSN"))
	redisAddress := strings.TrimSpace(os.Getenv("SUB2API_TEST_REDIS_ADDR"))
	var redisOptions *redis.Options
	expectedDatabase := ""
	waitForPostgres := func(db *sql.DB) {
		require.Eventually(t, func() bool {
			pingCtx, cancel := context.WithTimeout(ctx, time.Second)
			defer cancel()
			return db.PingContext(pingCtx) == nil
		}, 30*time.Second, 100*time.Millisecond, "disposable PostgreSQL must become ready")
	}
	if dsn != "" {
		var err error
		redisOptions, err = externalredis.Options(redisAddress, "SUB2API_TEST_REDIS_DB")
		require.NoError(t, err)
		// Other Go packages truncate tables, reset outbox sequences and install
		// fault triggers in the supplied database. Redis prefixes cannot isolate
		// those operations, and migrations explicitly address the public schema.
		// Give this fixture its own database before creating any business rows.
		adminDB, err := sql.Open("postgres", dsn)
		require.NoError(t, err)
		t.Cleanup(func() { require.NoError(t, adminDB.Close()) })
		waitForPostgres(adminDB)
		expectedDatabase = "seven_business_" + strings.ReplaceAll(uuid.NewString(), "-", "")
		created := false
		t.Cleanup(func() {
			if !created {
				return
			}
			dropCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			_, err := adminDB.ExecContext(dropCtx, "DROP DATABASE "+expectedDatabase+" WITH (FORCE)")
			require.NoError(t, err)
		})
		_, err = adminDB.ExecContext(ctx, "CREATE DATABASE "+expectedDatabase)
		require.NoError(t, err)
		created = true
		if strings.HasPrefix(dsn, "postgres://") || strings.HasPrefix(dsn, "postgresql://") {
			parsed, err := url.Parse(dsn)
			require.NoError(t, err)
			parsed.Path, parsed.RawPath = "/"+expectedDatabase, ""
			query := parsed.Query()
			query.Del("dbname")
			parsed.RawQuery = query.Encode()
			dsn = parsed.String()
		} else {
			dsn += " dbname=" + expectedDatabase
		}
	} else {
		postgresContainer, err := tcpostgres.Run(ctx, "postgres:18.1-alpine3.23",
			tcpostgres.WithDatabase("seven_business"), tcpostgres.WithUsername("postgres"),
			tcpostgres.WithPassword("disposable-test-password"), tcpostgres.BasicWaitStrategies())
		require.NoError(t, err)
		t.Cleanup(func() { require.NoError(t, postgresContainer.Terminate(context.Background())) })
		dsn, err = postgresContainer.ConnectionString(ctx, "sslmode=disable", "TimeZone=UTC")
		require.NoError(t, err)
		redisContainer, err := tcredis.Run(ctx, "redis:8.4-alpine")
		require.NoError(t, err)
		t.Cleanup(func() { require.NoError(t, redisContainer.Terminate(context.Background())) })
		host, err := redisContainer.Host(ctx)
		require.NoError(t, err)
		port, err := redisContainer.MappedPort(ctx, "6379/tcp")
		require.NoError(t, err)
		redisOptions = &redis.Options{Addr: net.JoinHostPort(host, port.Port())}
	}
	db, err := sql.Open("postgres", dsn)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	waitForPostgres(db)
	if expectedDatabase != "" {
		var actualDatabase string
		require.NoError(t, db.QueryRowContext(ctx, "SELECT current_database()").Scan(&actualDatabase))
		require.Equal(t, expectedDatabase, actualDatabase, "business fixtures must never reuse another package's database")
	}
	migrationCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	require.NoError(t, repository.ApplyMigrations(migrationCtx, db))
	client := dbent.NewClient(dbent.Driver(entsql.OpenDB(dialect.Postgres, db)))
	settingRepo := repository.NewSettingRepository(client)
	_, ok := settingRepo.(service.OutboundIDStore)
	require.True(t, ok, "production settings must provide persistent outbound identity mappings")
	service.ProvideSettingService(nil, settingRepo, repository.NewGroupRepository(client, db), repository.NewProxyRepository(client, db), &config.Config{})
	t.Cleanup(func() {
		service.SetDefaultOutboundIDStore(nil)
		outboundidentity.SetDefaultResolver(nil)
		zcode.SetDeviceIDResolver(nil)
		antigravity.SetUserAgentVersionResolver(nil)
	})
	rdb := redis.NewClient(redisOptions)
	t.Cleanup(func() { require.NoError(t, rdb.Close()) })
	require.NoError(t, rdb.Ping(ctx).Err())
	return &sevenBusinessServices{db: db, ent: client, redis: rdb, dsn: dsn}
}

// A namespace changes keys only. Every operation, Lua script and result still
// belongs to real Redis, including checks made from a second client instance.
type sevenBusinessRedisNamespace struct{ prefix string }

func (h sevenBusinessRedisNamespace) DialHook(next redis.DialHook) redis.DialHook { return next }

func (h sevenBusinessRedisNamespace) ProcessHook(next redis.ProcessHook) redis.ProcessHook {
	return func(ctx context.Context, command redis.Cmder) error {
		h.prefixKeys(command)
		return next(ctx, command)
	}
}

func (h sevenBusinessRedisNamespace) ProcessPipelineHook(next redis.ProcessPipelineHook) redis.ProcessPipelineHook {
	return func(ctx context.Context, commands []redis.Cmder) error {
		for _, command := range commands {
			h.prefixKeys(command)
		}
		return next(ctx, commands)
	}
}

func (h sevenBusinessRedisNamespace) prefixKeys(command redis.Cmder) {
	args := command.Args()
	prefix := func(index int) {
		if index < len(args) {
			key := fmt.Sprint(args[index])
			if key != "" && !strings.HasPrefix(key, h.prefix) {
				args[index] = h.prefix + key
			}
		}
	}
	switch strings.ToLower(command.Name()) {
	case "get", "set", "setnx", "setex", "psetex", "incr", "decr", "incrby", "expire", "pexpire", "ttl", "pttl", "exists", "hgetall", "hget", "hset", "hdel", "hincrbyfloat", "zadd", "zcard", "zrange", "zrangebyscore", "zrem", "zremrangebyscore", "zscore":
		prefix(1)
	case "mget", "del", "unlink":
		for index := 1; index < len(args); index++ {
			prefix(index)
		}
	case "eval", "evalsha", "eval_ro", "evalsha_ro":
		if len(args) >= 3 {
			count, _ := strconv.Atoi(fmt.Sprint(args[2]))
			for index := 0; index < count; index++ {
				prefix(3 + index)
			}
		}
	}
}

func (s *sevenBusinessServices) namespacedRedis(t *testing.T, prefix string) *redis.Client {
	t.Helper()
	options := *s.redis.Options()
	client := redis.NewClient(&options)
	client.AddHook(sevenBusinessRedisNamespace{prefix: prefix})
	t.Cleanup(func() { require.NoError(t, client.Close()) })
	return client
}

func (s *sevenBusinessServices) cleanRedisNamespace(t *testing.T, prefix string) {
	t.Helper()
	var cursor uint64
	for {
		keys, next, err := s.redis.Scan(context.Background(), cursor, prefix+"*", 100).Result()
		require.NoError(t, err)
		if len(keys) > 0 {
			require.NoError(t, s.redis.Unlink(context.Background(), keys...).Err())
		}
		cursor = next
		if cursor == 0 {
			return
		}
	}
}

type sevenBusinessSlots struct {
	account, other, user []string
	err                  error
	route                service.OpenAISessionRoute
}

type sevenBusinessWire struct {
	method, host, path string
	query, connectHost string
	headers            http.Header
	body               []byte
	slots              sevenBusinessSlots
}

type sevenBusinessProxy struct {
	server        *httptest.Server
	mu            sync.Mutex
	status        int
	terminalQuota bool
	wire          []sevenBusinessWire
	observe       func() sevenBusinessSlots
	native        *sevenNativeProxy
}

func newSevenBusinessProxy(t *testing.T, status int) *sevenBusinessProxy {
	t.Helper()
	proxy := &sevenBusinessProxy{status: status}
	proxy.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		body, err := io.ReadAll(request.Body)
		if err != nil {
			http.Error(w, "request body unavailable", http.StatusBadRequest)
			return
		}
		proxy.mu.Lock()
		observe := proxy.observe
		proxy.mu.Unlock()
		var slots sevenBusinessSlots
		if observe != nil {
			slots = observe()
		}
		proxy.mu.Lock()
		proxy.wire = append(proxy.wire, sevenBusinessWire{method: request.Method, host: request.URL.Host, path: request.URL.Path, headers: request.Header.Clone(), body: body, slots: slots})
		status, number, terminalQuota := proxy.status, len(proxy.wire), proxy.terminalQuota
		proxy.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Request-ID", fmt.Sprintf("seven-business-upstream-%d", number))
		if status != http.StatusOK {
			w.WriteHeader(status)
			if terminalQuota {
				_, _ = io.WriteString(w, `{"error":{"type":"insufficient_quota","code":"insufficient_quota","message":"Account quota exhausted"}}`)
			} else {
				_, _ = io.WriteString(w, `{"error":{"type":"upstream_unavailable","message":"temporarily unavailable"}}`)
			}
			return
		}
		_, _ = io.WriteString(w, `{"id":"chat-business","object":"chat.completion","model":"business-chat","choices":[{"index":0,"message":{"role":"assistant","content":"business success"},"finish_reason":"stop"}],"usage":{"prompt_tokens":10,"completion_tokens":5,"total_tokens":15}}`)
	}))
	t.Cleanup(proxy.server.Close)
	return proxy
}

func (p *sevenBusinessProxy) snapshot() []sevenBusinessWire {
	if p.native != nil {
		wire, _ := p.native.snapshot()
		result := make([]sevenBusinessWire, 0, len(wire))
		for _, request := range wire {
			headers := request.request.Header.Clone()
			headers.Set("Proxy-Authorization", request.proxyAuthorization)
			result = append(result, sevenBusinessWire{method: request.request.Method, host: request.request.Host,
				path: request.request.URL.Path, query: request.request.URL.RawQuery, connectHost: request.connectHost,
				headers: headers, body: request.body, slots: request.slots})
		}
		return result
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]sevenBusinessWire(nil), p.wire...)
}

func (p *sevenBusinessProxy) savedProxy(t *testing.T, repo service.ProxyRepository, owner string) *service.Proxy {
	t.Helper()
	endpoint, err := url.Parse(p.server.URL)
	require.NoError(t, err)
	port, err := strconv.Atoi(endpoint.Port())
	require.NoError(t, err)
	proxy := &service.Proxy{Name: "seven-business-" + owner + "-" + uuid.NewString(), Protocol: "http", Host: endpoint.Hostname(), Port: port,
		Username: "proxy-" + owner, Password: "password-" + owner, Status: service.StatusActive, FallbackMode: "none", ExpiryWarnDays: 7}
	require.NoError(t, repo.Create(context.Background(), proxy))
	return proxy
}

func sevenBusinessObserveSlots(client *redis.Client, owner, other, user int64) func() sevenBusinessSlots {
	return func() sevenBusinessSlots {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		var snapshot sevenBusinessSlots
		snapshot.account, snapshot.err = client.ZRange(ctx, fmt.Sprintf("concurrency:account:%d", owner), 0, -1).Result()
		if snapshot.err == nil {
			snapshot.other, snapshot.err = client.ZRange(ctx, fmt.Sprintf("concurrency:account:%d", other), 0, -1).Result()
		}
		if snapshot.err == nil {
			snapshot.user, snapshot.err = client.ZRange(ctx, fmt.Sprintf("concurrency:user:%d", user), 0, -1).Result()
		}
		return snapshot
	}
}

type sevenBusinessProvider struct {
	platform, preset, version, userAgent string
	companions                           map[string]string
}

type sevenBusinessGateway struct {
	server   *httptest.Server
	finished chan struct{}
}

func sevenBusinessCredentials(provider sevenBusinessProvider, owner, baseURL string) map[string]any {
	credentials := map[string]any{"api_key": "business-key-" + owner, "base_url": baseURL, "api_protocol": "chat_completions", "model_mapping": map[string]any{"business-chat": "business-chat"}}
	if provider.platform == "openai" {
		credentials["user_agent"] = provider.userAgent
	} else {
		credentials["outbound_identity"] = map[string]any{"preset": provider.preset, "user_agent": provider.userAgent, "version": provider.version}
	}
	return credentials
}

// Business rates are independently specified: ten input tokens at $2/million
// plus five output tokens at $4/million cost $0.00004000 before the group's
// 1.5 multiplier and $0.00006000 after it. This test observes the complete real
// HTTP -> account selection -> Redis leases/session -> PostgreSQL debit chain.
func TestSevenProviderFailoverPersistsRedisAffinityAndPostgresDebits(t *testing.T) {
	gin.SetMode(gin.TestMode)
	services := newSevenBusinessServices(t)
	providers := []sevenBusinessProvider{
		// OUTBOUND_IDENTITY.md: Native OpenAI API-key inference declares the
		// same selected Codex triple as OAuth inference.
		{"openai", "codex", "0.165.0", "codex_cli_rs/0.165.0 (Ubuntu 24.04; x86_64) xterm-256color", map[string]string{"Originator": "codex_cli_rs", "Version": "0.165.0"}},
		{"grok", "grok", "1.0.45", "grok-shell/1.0.45 (linux; x86_64)", map[string]string{"X-Grok-Client-Identifier": "grok-shell", "X-Grok-Client-Version": "1.0.45"}},
		{"deepseek", "deepseek", "0.2.1-rc.3", "deepseek-harness/0.2.1-rc.3 (+https://github.com/deepseek-ai/deepseek-harness)", nil},
		{"kimi", "kimi", "2.3.1", "kimi-code-cli/2.3.1", map[string]string{"X-Msh-Platform": "kimi_code_cli", "X-Msh-Version": "2.3.1", "X-Stainless-Package-Version": "6.34.0"}},
		{"minimax", "minimax_apikey", "0.91.1", "Anthropic/JS 0.91.1", map[string]string{"X-Stainless-Package-Version": "0.91.1"}},
		{"stepfun", "stepfun", "", "stepcode (linux 6.8.0-31-generic; x64)", map[string]string{"X-Step-Client": "stepcode", "X-Stainless-Package-Version": "6.40.0"}},
		{"zhipu", "zcode", "3.16.2", "ZCode/3.16.2 ai/6.0.193 ai-sdk/provider-utils/4.0.39 runtime/node.js/22", map[string]string{"X-ZCode-App-Version": "3.16.2"}},
	}
	for _, provider := range providers {
		for _, failure := range []int{http.StatusServiceUnavailable, http.StatusTooManyRequests} {
			t.Run(fmt.Sprintf("%s/%d", provider.platform, failure), func(t *testing.T) {
				runSevenBusinessFailover(t, services, provider, failure)
			})
		}
	}
}

func runSevenBusinessFailover(t *testing.T, services *sevenBusinessServices, provider sevenBusinessProvider, failure int) {
	runSevenBusinessFlow(t, services, provider, failure, nil)
}

func runSevenBusinessFlow(t *testing.T, services *sevenBusinessServices, provider sevenBusinessProvider, failure int, native *sevenNativeScenario) {
	t.Helper()
	ctx := context.Background()
	prefix := "seven-business:" + uuid.NewString() + ":"
	t.Cleanup(func() { services.cleanRedisNamespace(t, prefix) })
	redisOne := services.namespacedRedis(t, prefix)
	redisTwo := services.namespacedRedis(t, prefix)
	accountRepo := repository.NewAccountRepository(services.ent, services.db, nil)
	accountCreator, ok := accountRepo.(service.AccountDuplicateRepository)
	require.True(t, ok, "production accounts must support atomic group binding")
	userRepo := repository.NewUserRepository(services.ent, services.db)
	apiKeyRepo := repository.NewAPIKeyRepository(services.ent, services.db)
	groupRepo := repository.NewGroupRepository(services.ent, services.db)
	proxyRepo := repository.NewProxyRepository(services.ent, services.db)
	channelRepo := repository.NewChannelRepository(services.db)
	billingRepo := repository.NewUsageBillingRepository(services.ent, services.db)
	usageRepo := repository.NewUsageLogRepository(services.ent, services.db)
	user := &service.User{Email: "seven-business-" + uuid.NewString() + "@example.com", PasswordHash: "fixture-password-hash", Role: service.RoleUser,
		Status: service.StatusActive, Balance: 100, Concurrency: 2}
	require.NoError(t, userRepo.Create(ctx, user))
	t.Cleanup(func() {
		_, err := services.db.ExecContext(ctx, "DELETE FROM users WHERE id=$1", user.ID)
		require.NoError(t, err)
	})
	group := &service.Group{Name: "seven-business-" + uuid.NewString(), Platform: provider.platform, Status: service.StatusActive,
		SubscriptionType: service.SubscriptionTypeStandard, RateMultiplier: 1.5}
	require.NoError(t, groupRepo.Create(ctx, group))
	t.Cleanup(func() {
		_, err := services.db.ExecContext(ctx, "DELETE FROM openai_session_routes WHERE authz_domain=$1", fmt.Sprintf("group:%d", group.ID))
		require.NoError(t, err)
		_, err = services.db.ExecContext(ctx, "DELETE FROM groups WHERE id=$1", group.ID)
		require.NoError(t, err)
	})
	var directDials atomic.Int64
	destination := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { http.Error(w, "proxy bypass", http.StatusBadGateway) }))
	destination.Config.ConnState = func(_ net.Conn, state http.ConnState) {
		if state == http.StateNew {
			directDials.Add(1)
		}
	}
	destination.Start()
	t.Cleanup(destination.Close)
	var proxyA, proxyB *sevenBusinessProxy
	if native == nil {
		proxyA, proxyB = newSevenBusinessProxy(t, failure), newSevenBusinessProxy(t, http.StatusOK)
		proxyA.terminalQuota = provider.platform == "openai" && failure == http.StatusTooManyRequests
	} else {
		proxyA, proxyB = newSevenBusinessNativeProxy(t, native, failure), newSevenBusinessNativeProxy(t, native, http.StatusOK)
	}
	accounts := make([]*service.Account, 0, 2)
	for index, proxy := range []*sevenBusinessProxy{proxyA, proxyB} {
		owner := []string{"a", "b"}[index]
		savedProxy := proxy.savedProxy(t, proxyRepo, owner)
		t.Cleanup(func() {
			_, err := services.db.ExecContext(ctx, "DELETE FROM proxies WHERE id=$1", savedProxy.ID)
			require.NoError(t, err)
		})
		account := &service.Account{Name: "seven-business-" + owner + "-" + uuid.NewString(), Platform: provider.platform, Type: service.AccountTypeAPIKey,
			Status: service.StatusActive, Schedulable: true, Concurrency: 3, Priority: index * 10, ProxyID: &savedProxy.ID,
			Credentials: sevenBusinessCredentials(provider, owner, destination.URL),
			Extra:       map[string]any{"openai_responses_supported": false, "quota_limit": 100.0}, GroupIDs: []int64{group.ID}}
		if native != nil {
			account.Type = service.AccountTypeOAuth
			if native.accountKind != "" {
				account.Type = native.accountKind
			}
			account.Credentials = native.credentials(t, provider, owner)
			account.Extra["openai_responses_supported"] = true
		}
		require.NoError(t, accountCreator.CreateWithAccountGroups(ctx, account, []service.AccountGroup{{GroupID: group.ID, Priority: index * 10}}))
		accounts = append(accounts, account)
		t.Cleanup(func() {
			_, err := services.db.ExecContext(ctx, "DELETE FROM scheduler_outbox WHERE account_id=$1", account.ID)
			require.NoError(t, err)
			_, err = services.db.ExecContext(ctx, "DELETE FROM accounts WHERE id=$1", account.ID)
			require.NoError(t, err)
		})
	}
	apiKey := &service.APIKey{UserID: user.ID, Key: "sk-seven-business-" + uuid.NewString(), Name: "seven-business", Status: service.StatusActive,
		GroupID: &group.ID, Quota: 100, RateLimit5h: 100, RateLimit1d: 100, RateLimit7d: 100}
	require.NoError(t, apiKeyRepo.Create(ctx, apiKey))
	t.Cleanup(func() {
		for _, query := range []string{"DELETE FROM usage_logs WHERE api_key_id=$1", "DELETE FROM usage_billing_dedup WHERE api_key_id=$1", "DELETE FROM api_keys WHERE id=$1"} {
			_, err := services.db.ExecContext(ctx, query, apiKey.ID)
			require.NoError(t, err)
		}
	})
	inputPrice, outputPrice := 0.000002, 0.000004
	pricedModels := []string{"business-chat"}
	if native != nil {
		// Price the actual native model as well: Codex server-model correction
		// and Step Plan's native billing policy legitimately select that ID.
		pricedModels = append(pricedModels, native.model)
	}
	channel := &service.Channel{Name: "seven-business-" + uuid.NewString(), Status: service.StatusActive, BillingModelSource: service.BillingModelSourceRequested,
		GroupIDs: []int64{group.ID}, ModelPricing: []service.ChannelModelPricing{{Platform: provider.platform, Models: pricedModels, BillingMode: service.BillingModeToken, InputPrice: &inputPrice, OutputPrice: &outputPrice}}}
	require.NoError(t, channelRepo.Create(ctx, channel))
	t.Cleanup(func() { require.NoError(t, channelRepo.Delete(ctx, channel.ID)) })
	proxyA.mu.Lock()
	proxyA.observe = sevenBusinessObserveSlots(redisOne, accounts[0].ID, accounts[1].ID, user.ID)
	proxyA.mu.Unlock()
	proxyB.mu.Lock()
	proxyB.observe = sevenBusinessObserveSlots(redisOne, accounts[1].ID, accounts[0].ID, user.ID)
	proxyB.mu.Unlock()
	if provider.platform == "openai" {
		for _, proxy := range []*sevenBusinessProxy{proxyA, proxyB} {
			observeSlots := proxy.observe
			proxy.observe = func() sevenBusinessSlots {
				observed := observeSlots()
				if observed.err == nil {
					observed.route, observed.err = sevenBusinessCanonicalRoute(ctx, services.db, group.ID)
				}
				return observed
			}
		}
	}
	if native != nil {
		proxyA.native.observe = proxyA.observe
		proxyB.native.observe = proxyB.observe
	}
	cfg := &config.Config{}
	cfg.Gateway.MaxAccountSwitches = 2
	cfg.Gateway.Scheduling.LoadBatchEnabled = false
	cfg.Security.URLAllowlist.Enabled = false
	cfg.Security.URLAllowlist.AllowPrivateHosts = true
	cfg.Security.URLAllowlist.AllowInsecureHTTP = true
	cfg.Default.RateMultiplier = 1
	newGateway := func(rdb *redis.Client) *sevenBusinessGateway {
		// Distinct clients, caches, service instances and TCP listeners share
		// only real PostgreSQL rows and the namespaced Redis server.
		concurrency := service.NewConcurrencyService(repository.NewConcurrencyCache(rdb, 15, 30))
		billingCache := service.NewBillingCacheService(repository.NewBillingCache(rdb), userRepo, nil, apiKeyRepo, nil, nil, cfg, nil)
		t.Cleanup(billingCache.Stop)
		apiKeys := service.NewAPIKeyService(apiKeyRepo, userRepo, groupRepo, nil, nil, repository.NewAPIKeyCache(rdb), cfg)
		apiKeys.SetConcurrencyService(concurrency)
		channelService := service.NewChannelService(channelRepo, groupRepo, nil, nil, nil)
		billingService := service.NewBillingService(cfg, nil)
		gateway := service.NewOpenAIGatewayService(accountRepo, usageRepo, billingRepo, userRepo, nil, nil,
			repository.NewGatewayCache(rdb), cfg, nil, concurrency, billingService, nil, billingCache, repository.NewHTTPUpstream(cfg),
			service.NewDeferredService(accountRepo, nil, time.Second), service.NewOpenAITokenProvider(accountRepo, nil, nil), service.NewGrokTokenProvider(accountRepo, nil), service.NewModelPricingResolver(channelService, billingService), channelService, nil, nil, nil, repository.ProvideOpenAISessionRouteStore(services.db, rdb))
		if native != nil {
			// Use the public production wiring entrypoint, including the native
			// request preparer; do not substitute a test transport or issuer.
			service.ProvideCNOAuthService(proxyRepo, accountRepo, nil, rdb, nil, gateway,
				&service.AccountTestService{}, &service.CNProviderQuotaService{}, nil)
		}
		gatewayHandler := handler.NewOpenAIGatewayHandler(gateway, concurrency, billingCache, apiKeys, nil, nil, nil, nil, cfg)
		router := gin.New()
		router.Use(middleware.ClientRequestID(), gin.HandlerFunc(middleware.NewAPIKeyAuthMiddleware(apiKeys, nil, cfg)))
		router.Use(func(c *gin.Context) {
			c.Next()
			if c.Writer.Status() >= 400 {
				if raw, ok := c.Get(service.OpsUpstreamErrorsKey); ok {
					if events, ok := raw.([]*service.OpsUpstreamErrorEvent); ok {
						for _, event := range events {
							t.Logf("gateway failed stage=%s reason=%s account=%d message=%s detail=%s", event.Stage, event.Reason, event.AccountID, event.Message, event.Detail)
						}
					}
				}
			}
		})
		router.POST("/v1/chat/completions", gatewayHandler.ChatCompletions)
		router.POST("/v1/responses", gatewayHandler.Responses)
		router.POST("/v1/messages", gatewayHandler.Messages)
		finished := make(chan struct{}, 1)
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			router.ServeHTTP(w, r)
			finished <- struct{}{}
		}))
		t.Cleanup(server.Close)
		return &sevenBusinessGateway{server: server, finished: finished}
	}
	request := func(gateway *sevenBusinessGateway, turn int) string {
		requestCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
		body := fmt.Sprintf(`{"model":"business-chat","messages":[{"role":"user","content":"business turn %d"}]}`, turn)
		path := "/v1/chat/completions"
		if native != nil {
			body, path = native.request(turn), native.ingress
		}
		req, err := http.NewRequestWithContext(requestCtx, http.MethodPost, gateway.server.URL+path, strings.NewReader(body))
		require.NoError(t, err)
		req.Header.Set("Authorization", "Bearer "+apiKey.Key)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("session_id", "seven-business-session")
		req.Header.Set("User-Agent", "untrusted-browser/99")
		req.Header.Set("Originator", "untrusted-origin")
		req.Header.Set("Version", "99.99.99")
		req.Header.Set("X-Stainless-Package-Version", "99.99.99")
		response, err := gateway.server.Client().Do(req)
		require.NoError(t, err)
		defer func() { require.NoError(t, response.Body.Close()) }()
		responseBody, err := io.ReadAll(response.Body)
		require.NoError(t, err)
		// A flushed response body can arrive before synchronous post-billing and
		// deferred lease release. Observe handler completion before checking SQL.
		select {
		case <-gateway.finished:
		case <-requestCtx.Done():
			t.Fatal("gateway did not finish billing and release after its response")
		}
		if response.StatusCode != http.StatusOK && native != nil {
			for index, proxy := range []*sevenBusinessProxy{proxyA, proxyB} {
				wire, proxyErrors := proxy.native.snapshot()
				t.Logf("native proxy %d: requests=%d errors=%v", index, len(wire), proxyErrors)
			}
		}
		require.Equal(t, http.StatusOK, response.StatusCode, string(responseBody))
		if native == nil {
			require.Contains(t, string(responseBody), "business success")
		} else {
			native.assertResult(t, responseBody)
		}
		requestID := response.Header.Get("X-Client-Request-ID")
		require.NotEmpty(t, requestID)
		return "client:" + requestID
	}
	firstRequestID := request(newGateway(redisOne), 1)
	require.Len(t, proxyA.snapshot(), 1)
	require.Len(t, proxyB.snapshot(), 1)
	sevenBusinessAssertDebits(t, services.db, user.ID, apiKey.ID, accounts, 1)
	sevenBusinessAssertReleased(t, redisTwo, user.ID, accounts)
	var committedRoute service.OpenAISessionRoute
	if provider.platform == "openai" {
		var routeCount int
		require.NoError(t, services.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM openai_session_routes WHERE authz_domain=$1`, fmt.Sprintf("group:%d", group.ID)).Scan(&routeCount))
		require.Equal(t, 1, routeCount, "the logical session must have exactly one canonical PostgreSQL route")
		var err error
		committedRoute, err = sevenBusinessCanonicalRoute(ctx, services.db, group.ID)
		require.NoError(t, err)
		require.Equal(t, accounts[1].ID, committedRoute.OwnerAccountID)
		require.Equal(t, int64(2), committedRoute.Generation, "one successful A to B migration must commit exactly once")
		require.Equal(t, service.OpenAISessionRouteActive, committedRoute.Status)
		require.Zero(t, committedRoute.CandidateAccountID)
		require.Empty(t, committedRoute.LeaseID)
		require.True(t, committedRoute.LeaseExpiresAt.IsZero())
		require.Len(t, committedRoute.SessionDigest, 64)
		require.NotContains(t, committedRoute.SessionDigest, "seven-business-session")
		secondStore := repository.ProvideOpenAISessionRouteStore(services.db, redisTwo)
		observed, err := secondStore.Get(ctx, committedRoute.AuthzDomain, committedRoute.SessionDigest)
		require.NoError(t, err)
		require.Equal(t, committedRoute, observed, "a second production store must read B and its committed generation")
	} else {
		keys, err := services.redis.Keys(ctx, prefix+fmt.Sprintf("sticky_session:%d:*", group.ID)).Result()
		require.NoError(t, err)
		require.Len(t, keys, 1, "the successful fallback must leave one real Redis session binding")
		boundOwner, err := services.redis.Get(ctx, keys[0]).Int64()
		require.NoError(t, err)
		require.Equal(t, accounts[1].ID, boundOwner)
		sessionHash := strings.TrimPrefix(keys[0], prefix+fmt.Sprintf("sticky_session:%d:", group.ID))
		boundOwner, err = repository.NewGatewayCache(redisTwo).GetSessionAccountID(ctx, group.ID, sessionHash)
		require.NoError(t, err)
		require.Equal(t, accounts[1].ID, boundOwner, "a second production Redis client must see B")
	}
	// Restore A's actual persisted admission state and upstream before creating
	// a second gateway. Its higher priority must lose to the saved B session.
	_, err := services.db.ExecContext(ctx, `UPDATE accounts SET status='active',schedulable=TRUE,error_message='',temp_unschedulable_until=NULL,temp_unschedulable_reason='',rate_limited_at=NULL,rate_limit_reset_at=NULL,overload_until=NULL WHERE id=$1`, accounts[0].ID)
	require.NoError(t, err)
	recovered, err := accountRepo.GetByID(ctx, accounts[0].ID)
	require.NoError(t, err)
	require.True(t, recovered.Schedulable)
	require.Equal(t, service.StatusActive, recovered.Status)
	require.Less(t, recovered.Priority, accounts[1].Priority)
	proxyA.mu.Lock()
	proxyA.status = http.StatusOK
	proxyA.mu.Unlock()
	secondRequestID := request(newGateway(redisTwo), 2)
	require.NotEqual(t, firstRequestID, secondRequestID)
	require.Len(t, proxyA.snapshot(), 1, "recovered higher-priority A must not steal B's Redis session")
	require.Len(t, proxyB.snapshot(), 2)
	if provider.platform == "openai" {
		current, err := sevenBusinessCanonicalRoute(ctx, services.db, group.ID)
		require.NoError(t, err)
		require.Equal(t, committedRoute, current, "the second gateway must keep B without another generation or migration")
		first, candidate, resumed := proxyA.snapshot()[0].slots.route, proxyB.snapshot()[0].slots.route, proxyB.snapshot()[1].slots.route
		require.Equal(t, accounts[0].ID, first.OwnerAccountID)
		require.Equal(t, int64(1), first.Generation)
		require.Equal(t, service.OpenAISessionRouteActive, first.Status)
		require.Zero(t, first.CandidateAccountID)
		require.Equal(t, accounts[0].ID, candidate.OwnerAccountID, "B must send under a migration lease before semantic acceptance")
		require.Equal(t, int64(1), candidate.Generation)
		require.Equal(t, service.OpenAISessionRouteMigrating, candidate.Status)
		require.Equal(t, accounts[1].ID, candidate.CandidateAccountID)
		require.NotEmpty(t, candidate.LeaseID)
		require.False(t, candidate.LeaseExpiresAt.IsZero())
		require.Equal(t, committedRoute, resumed, "the resumed turn must send with B as the active canonical owner")
	}
	require.Zero(t, directDials.Load(), "every credential owner must retain its saved proxy")
	sevenBusinessAssertReleased(t, redisOne, user.ID, accounts)
	sevenBusinessAssertDebits(t, services.db, user.ID, apiKey.ID, accounts, 2)
	logical, err := url.Parse(destination.URL)
	require.NoError(t, err)
	var allSlots []sevenBusinessSlots
	for index, proxy := range []*sevenBusinessProxy{proxyA, proxyB} {
		owner := []string{"a", "b"}[index]
		for requestIndex, wire := range proxy.snapshot() {
			require.Equal(t, http.MethodPost, wire.method)
			if native == nil {
				require.Equal(t, logical.Host, wire.host)
				require.Equal(t, "/v1/chat/completions", wire.path)
				require.Equal(t, "Bearer business-key-"+owner, wire.headers.Get("Authorization"))
			} else {
				turn := 1
				if index == 1 && requestIndex == 1 {
					turn = 2
				}
				native.assertWire(t, owner, turn, wire)
			}
			require.Equal(t, "Basic "+base64.StdEncoding.EncodeToString([]byte("proxy-"+owner+":password-"+owner)), wire.headers.Get("Proxy-Authorization"))
			require.Equal(t, provider.userAgent, wire.headers.Get("User-Agent"))
			for name, expected := range provider.companions {
				require.Equal(t, expected, wire.headers.Get(name), name)
			}
			require.NotEqual(t, "untrusted-origin", wire.headers.Get("Originator"))
			require.NotEqual(t, "99.99.99", wire.headers.Get("Version"))
			var payload map[string]any
			require.NoError(t, json.Unmarshal(wire.body, &payload))
			if native == nil {
				require.Equal(t, "business-chat", payload["model"])
				require.Len(t, payload["messages"], 1)
			}
			require.NoError(t, wire.slots.err)
			require.Len(t, wire.slots.account, 1, "the credential owner holds its real Redis account lease at send time")
			require.Empty(t, wire.slots.other, "failed account capacity must be released before fallback")
			require.Len(t, wire.slots.user, 1)
			allSlots = append(allSlots, wire.slots)
		}
	}
	if native != nil {
		for _, proxy := range []*sevenBusinessProxy{proxyA, proxyB} {
			_, errors := proxy.native.snapshot()
			require.Empty(t, errors, "native proxy must finish verified HTTPS streams")
		}
	}
	require.Equal(t, allSlots[0].user, allSlots[1].user, "failover retains the first request's user lease")
	require.NotEqual(t, allSlots[1].user, allSlots[2].user, "the subsequent request acquires its own user lease")
	sevenBusinessAssertUsageAndDedup(t, services.db, billingRepo, user.ID, apiKey.ID, accounts[1], firstRequestID, secondRequestID)
	sevenBusinessAssertDebits(t, services.db, user.ID, apiKey.ID, accounts, 2)
}

func sevenBusinessCanonicalRoute(ctx context.Context, db *sql.DB, groupID int64) (service.OpenAISessionRoute, error) {
	var route service.OpenAISessionRoute
	var status string
	var leaseExpiry sql.NullTime
	err := db.QueryRowContext(ctx, `SELECT authz_domain, session_digest, owner_account_id, generation, route_status,
		migration_reason, COALESCE(candidate_account_id,0), lease_id, lease_expires_at
		FROM openai_session_routes WHERE authz_domain=$1`, fmt.Sprintf("group:%d", groupID)).Scan(
		&route.AuthzDomain, &route.SessionDigest, &route.OwnerAccountID, &route.Generation, &status,
		&route.MigrationReason, &route.CandidateAccountID, &route.LeaseID, &leaseExpiry)
	route.Status = service.OpenAISessionRouteStatus(status)
	if leaseExpiry.Valid {
		route.LeaseExpiresAt = leaseExpiry.Time
	}
	return route, err
}

func sevenBusinessAssertReleased(t *testing.T, client *redis.Client, userID int64, accounts []*service.Account) {
	t.Helper()
	for _, key := range []string{fmt.Sprintf("concurrency:account:%d", accounts[0].ID), fmt.Sprintf("concurrency:account:%d", accounts[1].ID), fmt.Sprintf("concurrency:user:%d", userID)} {
		count, err := client.ZCard(context.Background(), key).Result()
		require.NoError(t, err)
		require.Zero(t, count, "all slots must release immediately: %s", key)
	}
}

func sevenBusinessAssertDebits(t *testing.T, db *sql.DB, userID, keyID int64, accounts []*service.Account, successes int) {
	t.Helper()
	// Exact SQL decimal strings avoid validating the price calculation with the
	// implementation's calculator or hiding rounding errors inside a tolerance.
	expectedBalance := map[int]string{1: "99.99994000", 2: "99.99988000"}[successes]
	expectedKeyUsage := map[int]string{1: "0.00006000", 2: "0.00012000"}[successes]
	expectedAccountUsage := map[int]string{1: "0.00004000", 2: "0.00008000"}[successes]
	var balance, quota, window5h, window1d, window7d string
	require.NoError(t, db.QueryRowContext(context.Background(), "SELECT balance::text FROM users WHERE id=$1", userID).Scan(&balance))
	require.Equal(t, expectedBalance, balance)
	require.NoError(t, db.QueryRowContext(context.Background(), "SELECT quota_used::text,usage_5h::text,usage_1d::text,usage_7d::text FROM api_keys WHERE id=$1", keyID).Scan(&quota, &window5h, &window1d, &window7d))
	for _, actual := range []string{quota, window5h, window1d, window7d} {
		require.Equal(t, expectedKeyUsage, actual, "key quota and all enabled windows consume the exact successful charge")
	}
	for index, account := range accounts {
		var usage string
		require.NoError(t, db.QueryRowContext(context.Background(), "SELECT COALESCE((extra->>'quota_used')::numeric,0)::numeric(20,8)::text FROM accounts WHERE id=$1", account.ID).Scan(&usage))
		if index == 0 {
			require.Equal(t, "0.00000000", usage, "failed A must never consume provider-account quota")
		} else if account.Type == service.AccountTypeOAuth {
			require.Equal(t, "0.00000000", usage, "OAuth observation metadata is not an API-key monetary quota")
		} else {
			require.Equal(t, expectedAccountUsage, usage)
		}
	}
	var usageCount, dedupCount, failedAccountUsage int
	require.NoError(t, db.QueryRowContext(context.Background(), "SELECT COUNT(*),COUNT(*) FILTER(WHERE account_id=$2) FROM usage_logs WHERE api_key_id=$1", keyID, accounts[0].ID).Scan(&usageCount, &failedAccountUsage))
	require.NoError(t, db.QueryRowContext(context.Background(), "SELECT COUNT(*) FROM usage_billing_dedup WHERE api_key_id=$1", keyID).Scan(&dedupCount))
	require.Equal(t, successes, usageCount)
	require.Equal(t, successes, dedupCount)
	require.Zero(t, failedAccountUsage)
}

func sevenBusinessAssertUsageAndDedup(t *testing.T, db *sql.DB, billing service.UsageBillingRepository, userID, keyID int64, account *service.Account, requestIDs ...string) {
	t.Helper()
	for _, requestID := range requestIDs {
		var actualUser, actualAccount int64
		var model, actual, raw, completion, source, fingerprint string
		var inputTokens, outputTokens, dedupCount int
		require.NoError(t, db.QueryRowContext(context.Background(), `SELECT u.user_id,u.account_id,u.model,u.input_tokens,u.output_tokens,
			u.actual_cost::numeric(20,8)::text,u.total_cost::numeric(20,8)::text,u.completion_status,u.usage_source,d.request_fingerprint
			FROM usage_logs u JOIN usage_billing_dedup d ON d.request_id=u.request_id AND d.api_key_id=u.api_key_id
			WHERE u.api_key_id=$1 AND u.request_id=$2`, keyID, requestID).Scan(&actualUser, &actualAccount, &model, &inputTokens, &outputTokens, &actual, &raw, &completion, &source, &fingerprint))
		require.Equal(t, userID, actualUser)
		require.Equal(t, account.ID, actualAccount)
		require.Equal(t, "business-chat", model)
		require.Equal(t, 10, inputTokens)
		require.Equal(t, 5, outputTokens)
		require.Equal(t, "0.00006000", actual)
		require.Equal(t, "0.00004000", raw)
		require.Equal(t, service.UsageCompletionCompleted, completion)
		require.Equal(t, service.UsageSourceUpstreamExact, source)
		require.NotEmpty(t, fingerprint)
		require.NoError(t, db.QueryRowContext(context.Background(), "SELECT COUNT(*) FROM usage_billing_dedup WHERE api_key_id=$1 AND request_id=$2", keyID, requestID).Scan(&dedupCount))
		require.Equal(t, 1, dedupCount)
		// Replay the already committed key through the real repository. Its
		// persisted fingerprint is an opaque replay token, never a price oracle.
		command := &service.UsageBillingCommand{
			RequestID: requestID, RequestFingerprint: fingerprint, APIKeyID: keyID, UserID: userID, AccountID: account.ID,
			AccountType: account.Type, Model: "business-chat", InputTokens: 10, OutputTokens: 5,
			BalanceCost: 0.00006000, APIKeyQuotaCost: 0.00006000, APIKeyRateLimitCost: 0.00006000, AccountQuotaCost: 0.00004000,
		}
		if account.Type == service.AccountTypeOAuth {
			command.AccountQuotaCost = 0
		}
		result, err := billing.Apply(context.Background(), command)
		require.NoError(t, err)
		require.NotNil(t, result)
		require.False(t, result.Applied, "a committed success cannot be charged again after a billing retry")
	}
}
