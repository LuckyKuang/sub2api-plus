//go:build unit

package routes

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/config"
	"github.com/LuckyKuang/sub2api-plus/internal/handler"
	"github.com/LuckyKuang/sub2api-plus/internal/pkg/ctxkey"
	"github.com/LuckyKuang/sub2api-plus/internal/pkg/logger"
	"github.com/LuckyKuang/sub2api-plus/internal/pkg/openai_compat"
	"github.com/LuckyKuang/sub2api-plus/internal/pkg/tlsfingerprint"
	"github.com/LuckyKuang/sub2api-plus/internal/repository"
	middleware "github.com/LuckyKuang/sub2api-plus/internal/server/middleware"
	"github.com/LuckyKuang/sub2api-plus/internal/service"
	"github.com/alicebob/miniredis/v2"
	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

type globalCapacityRouteAccountRepo struct {
	service.AccountRepository
	accounts       []service.Account
	selectionReads atomic.Int32
}

func (r *globalCapacityRouteAccountRepo) ListSchedulableByPlatform(_ context.Context, platform string) ([]service.Account, error) {
	r.selectionReads.Add(1)
	var out []service.Account
	for _, a := range r.accounts {
		if a.Platform == platform {
			out = append(out, a)
		}
	}
	return out, nil
}
func (r *globalCapacityRouteAccountRepo) GetByID(_ context.Context, id int64) (*service.Account, error) {
	for i := range r.accounts {
		if r.accounts[i].ID == id {
			return &r.accounts[i], nil
		}
	}
	return nil, service.ErrAccountNotFound
}
func (r *globalCapacityRouteAccountRepo) ListModelAvailabilityCandidates(_ context.Context, _ *int64, platforms []string, _ bool) ([]service.Account, error) {
	var out []service.Account
	for _, platform := range platforms {
		accounts, err := r.ListSchedulableByPlatform(context.Background(), platform)
		if err != nil {
			return nil, err
		}
		out = append(out, accounts...)
	}
	return out, nil
}

type globalCapacityRouteObservedCache struct {
	service.ConcurrencyCache
	service.ConcurrencyLeaseCache
	service.APIKeyConcurrencyCache
	userQueueWrites atomic.Int32
	accountAcquires atomic.Int32
}

func (c *globalCapacityRouteObservedCache) IncrementWaitCount(ctx context.Context, userID int64, maxWait int) (bool, error) {
	c.userQueueWrites.Add(1)
	return c.ConcurrencyCache.IncrementWaitCount(ctx, userID, maxWait)
}
func (c *globalCapacityRouteObservedCache) AcquireAccountSlot(ctx context.Context, accountID int64, limit int, owner string) (bool, error) {
	c.accountAcquires.Add(1)
	return c.ConcurrencyCache.AcquireAccountSlot(ctx, accountID, limit, owner)
}

// The production identity resolver generates official-format candidates. The
// fixture implements its atomic first-writer mapping port without selecting or
// overriding identity headers itself.
type globalCapacityRouteIDStore struct{ ids sync.Map }

func (s *globalCapacityRouteIDStore) ResolveOutboundID(_ context.Context, namespace, digest, candidate string) (string, error) {
	value, _ := s.ids.LoadOrStore(namespace+":"+digest, candidate)
	resolved, ok := value.(string)
	if !ok {
		return "", fmt.Errorf("unexpected outbound identity mapping type %T", value)
	}
	return resolved, nil
}

type globalCapacityRouteUserRepo struct{ service.UserRepository }

func (globalCapacityRouteUserRepo) GetByID(context.Context, int64) (*service.User, error) {
	return &service.User{ID: 42, Concurrency: 3}, nil
}

type globalCapacityRouteUsageRepo struct{ service.UsageLogRepository }

func (globalCapacityRouteUsageRepo) Create(context.Context, *service.UsageLog) (bool, error) {
	return true, nil
}

type globalCapacityRouteHTTP struct{ client *http.Client }

func (u globalCapacityRouteHTTP) Do(req *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
	return u.client.Do(req)
}
func (u globalCapacityRouteHTTP) DoWithTLS(req *http.Request, p string, id int64, c int, _ *tlsfingerprint.Profile) (*http.Response, error) {
	return u.Do(req, p, id, c)
}

func TestGatewayRoutesGlobalCapacityIncludesAllKeysPlatformsAndAgentRequests(t *testing.T) {
	// Acceptance: with global C=3, an OpenAI parent and Grok/DeepSeek children
	// consume all capacity despite separate API keys/groups. A fourth authenticated
	// request cannot reach any provider; releasing one permits another request.
	gin.SetMode(gin.TestMode)
	logCore, observedLogs := observer.New(zap.WarnLevel)
	requestLog := zap.New(logCore)
	identityIDs := &globalCapacityRouteIDStore{}
	redisServer := miniredis.RunT(t)
	redisClient := redis.NewClient(&redis.Options{Addr: redisServer.Addr()})
	defer redisClient.Close()
	rawPool := repository.NewConcurrencyCache(redisClient, 15, 900)
	leasePool, ok := rawPool.(service.ConcurrencyLeaseCache)
	require.True(t, ok)
	keyPool, ok := rawPool.(service.APIKeyConcurrencyCache)
	require.True(t, ok)
	pool := &globalCapacityRouteObservedCache{ConcurrencyCache: rawPool, ConcurrencyLeaseCache: leasePool, APIKeyConcurrencyCache: keyPool}
	concurrency := service.NewConcurrencyService(pool)
	concurrency.SetUserRepository(globalCapacityRouteUserRepo{})
	platforms := []string{service.PlatformOpenAI, service.PlatformGrok, service.PlatformDeepseek}
	models := []string{"gpt-5.1", "grok-4.3", "deepseek-v4-flash"}
	started := make(chan string, 8)
	var providerCalls atomic.Int32
	gates := map[string]chan struct{}{}
	releases := map[string]*sync.Once{}
	for _, p := range platforms {
		gates[p] = make(chan struct{})
		releases[p] = &sync.Once{}
	}
	release := func(p string) { releases[p].Do(func() { close(gates[p]) }) }
	releaseAll := func() {
		for _, p := range platforms {
			release(p)
		}
	}
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		platform := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer upstream-")
		gate, exists := gates[platform]
		if !exists {
			http.Error(w, "unexpected credential", 500)
			return
		}
		providerCalls.Add(1)
		started <- platform
		select {
		case <-gate:
		case <-r.Context().Done():
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"capacity","object":"chat.completion","model":"gpt-5.1","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`)
	}))
	defer upstream.Close()
	defer releaseAll()
	cfg := &config.Config{RunMode: config.RunModeSimple, Security: config.SecurityConfig{URLAllowlist: config.URLAllowlistConfig{AllowInsecureHTTP: true}}}
	cfg.Gateway.MaxBodySize = 1 << 20
	cfg.Gateway.TextMaxBodySize = 1 << 20
	repo := &globalCapacityRouteAccountRepo{}
	groups := map[string]*service.Group{}
	for i, p := range platforms {
		group := &service.Group{ID: int64(i + 1), Platform: p, Status: service.StatusActive, Hydrated: true, RateMultiplier: 1}
		groups[p] = group
		account := service.Account{ID: int64(i + 1), Platform: p, Type: service.AccountTypeAPIKey, Status: service.StatusActive, Schedulable: true, Concurrency: 10, Credentials: map[string]any{"api_key": "upstream-" + p, "base_url": upstream.URL, "api_protocol": service.APIProtocolChatCompletions}, AccountGroups: []service.AccountGroup{{AccountID: int64(i + 1), GroupID: group.ID}}}
		if p == service.PlatformOpenAI {
			// Native OpenAI API keys use Responses unless their explicit protocol
			// mode selects Chat. This responder implements Chat Completions.
			account.Extra = map[string]any{openai_compat.ExtraKeyResponsesMode: string(openai_compat.ResponsesSupportModeForceChatCompletions)}
		}
		repo.accounts = append(repo.accounts, account)
	}
	billing := service.NewBillingCacheService(nil, nil, nil, nil, nil, nil, cfg, nil)
	defer billing.Stop()
	gw := service.NewOpenAIGatewayService(repo, globalCapacityRouteUsageRepo{}, nil, nil, nil, nil, nil, cfg, nil, concurrency, service.NewBillingService(cfg, nil), nil, billing, globalCapacityRouteHTTP{upstream.Client()}, &service.DeferredService{}, nil, nil, nil, nil, nil, nil, nil, nil)
	handlers := &handler.Handlers{Gateway: handler.NewGatewayHandler(nil, gw, nil, nil, nil, concurrency, billing, nil, &service.APIKeyService{}, nil, nil, nil, nil, cfg, nil), OpenAIGateway: handler.NewOpenAIGatewayHandler(gw, concurrency, billing, &service.APIKeyService{}, nil, nil, nil, nil, cfg), AsyncImage: handler.NewAsyncImageHandler(nil, nil)}
	router := gin.New()
	RegisterGatewayRoutes(router, handlers, middleware.APIKeyAuthMiddleware(func(c *gin.Context) {
		platform := c.GetHeader("X-Fixture-Platform")
		group := groups[platform]
		if group == nil {
			c.AbortWithStatus(http.StatusUnauthorized)
			return
		}
		keyID := int64(1)
		if c.GetHeader("Authorization") == "Bearer child-key" {
			keyID = 2
		} else if c.GetHeader("Authorization") == "Bearer second-child-key" {
			keyID = 3
		} else if c.GetHeader("Authorization") == "Bearer fourth-key" {
			keyID = 4
		}
		user := &service.User{ID: 42, Concurrency: 99, Balance: 100}
		c.Set(string(middleware.ContextKeyAPIKey), &service.APIKey{ID: keyID, UserID: 42, User: user, GroupID: &group.ID, Group: group})
		c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: 42, Concurrency: 99})
		c.Request = c.Request.WithContext(context.WithValue(c.Request.Context(), ctxkey.Group, group))
		c.Request = c.Request.WithContext(logger.IntoContext(c.Request.Context(), requestLog))
		c.Request = c.Request.WithContext(service.WithOutboundIDStore(c.Request.Context(), identityIDs))
		c.Next()
	}), nil, nil, nil, nil, nil, cfg)
	gateway := httptest.NewServer(router)
	defer gateway.Close()
	defer releaseAll()
	client := gateway.Client()
	client.Timeout = 5 * time.Second
	send := func(ctx context.Context, key, platform, model, role string) (int, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, gateway.URL+"/v1/chat/completions", strings.NewReader(fmt.Sprintf(`{"model":%q,"messages":[{"role":"user","content":"hello"}]}`, model)))
		if err != nil {
			return 0, err
		}
		req.Header.Set("Authorization", "Bearer "+key)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Fixture-Platform", platform)
		req.Header.Set("X-Agent-Role", role)
		resp, err := client.Do(req)
		if err != nil {
			return 0, err
		}
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		if resp.StatusCode != 200 {
			return resp.StatusCode, fmt.Errorf("gateway returned %d: %s", resp.StatusCode, body)
		}
		return resp.StatusCode, nil
	}
	type outcome struct {
		status int
		err    error
	}
	results := make([]chan outcome, 3)
	prematureResults := make(chan string, 3)
	for i, p := range platforms {
		results[i] = make(chan outcome, 1)
		key := []string{"parent-key", "child-key", "second-child-key"}[i]
		role := []string{"main", "subagent-1", "subagent-2"}[i]
		go func(i int, p, key, role string) {
			status, err := send(context.Background(), key, p, models[i], role)
			results[i] <- outcome{status, err}
			prematureResults <- fmt.Sprintf("platform=%s status=%d error=%v", p, status, err)
		}(i, p, key, role)
	}
	observed := []string{}
	for len(observed) < 3 {
		select {
		case p := <-started:
			observed = append(observed, p)
		case result := <-prematureResults:
			t.Fatalf("an admitted request finished before provider capacity was released: %s; gateway diagnostics: %+v", result, observedLogs.All())
		case <-time.After(3 * time.Second):
			t.Fatal("the three admitted platform requests did not reach upstream")
		}
	}
	require.ElementsMatch(t, platforms, observed)
	count, err := pool.GetUserConcurrency(context.Background(), 42)
	require.NoError(t, err)
	require.Equal(t, 3, count)
	selectionReads := repo.selectionReads.Load()
	accountAcquires := pool.accountAcquires.Load()
	blocked, stopBlocked := context.WithTimeout(context.Background(), time.Second)
	rejectionStarted := time.Now()
	rejectedStatus, err := send(blocked, "fourth-key", platforms[0], models[0], "subagent-3")
	stopBlocked()
	require.Less(t, time.Since(rejectionStarted), time.Second)
	require.Error(t, err)
	require.Equal(t, http.StatusTooManyRequests, rejectedStatus, "the fourth request must receive HTTP 429 while the first three remain executing")
	require.NotErrorIs(t, err, context.DeadlineExceeded)
	require.EqualValues(t, 0, pool.userQueueWrites.Load(), "external requests must never enter the user waiting queue")
	require.Equal(t, selectionReads, repo.selectionReads.Load(), "a rejected user cannot select another account")
	require.Equal(t, accountAcquires, pool.accountAcquires.Load(), "a rejected user cannot acquire account capacity")
	require.EqualValues(t, 3, providerCalls.Load(), "the fourth key must not select and send another provider request")
	count, err = pool.GetUserConcurrency(context.Background(), 42)
	require.NoError(t, err)
	require.Equal(t, 3, count, "rejecting the fourth request must preserve all three admitted owners")
	keyCounts, err := concurrency.GetAPIKeyConcurrencyBatch(context.Background(), []int64{1, 2, 3, 4})
	require.NoError(t, err)
	require.Equal(t, 1, keyCounts[1])
	require.Equal(t, 1, keyCounts[2])
	require.Equal(t, 1, keyCounts[3])
	require.Zero(t, keyCounts[4], "the rejected key must not gain an execution record")
	for i, result := range results {
		select {
		case early := <-result:
			t.Fatalf("active request %d finished before release: %+v", i, early)
		default:
		}
	}
	release(platforms[0])
	first := <-results[0]
	require.NoError(t, first.err)
	require.Equal(t, 200, first.status)
	status, err := send(context.Background(), "fourth-key", platforms[0], models[0], "subagent-3")
	require.NoError(t, err)
	require.Equal(t, 200, status)
	require.EqualValues(t, 4, providerCalls.Load())
	releaseAll()
	for _, result := range results[1:] {
		got := <-result
		require.NoError(t, got.err)
		require.Equal(t, 200, got.status)
	}
	require.Eventually(t, func() bool {
		count, err := pool.GetUserConcurrency(context.Background(), 42)
		return err == nil && count == 0
	}, time.Second, 10*time.Millisecond)
}
