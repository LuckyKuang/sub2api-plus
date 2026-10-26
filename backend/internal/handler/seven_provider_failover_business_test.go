//go:build unit

package handler

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/config"
	"github.com/LuckyKuang/sub2api-plus/internal/pkg/zcode"
	"github.com/LuckyKuang/sub2api-plus/internal/repository"
	middleware "github.com/LuckyKuang/sub2api-plus/internal/server/middleware"
	"github.com/LuckyKuang/sub2api-plus/internal/service"
	"github.com/LuckyKuang/sub2api-plus/internal/testutil"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type sevenProviderSessionKey struct {
	group int64
	hash  string
}

type sevenProviderBindings struct {
	testutil.StubGatewayCache
	mu       sync.Mutex
	bindings map[sevenProviderSessionKey]int64
	hits     []int64
}

func (s *sevenProviderBindings) GetSessionAccountID(_ context.Context, group int64, hash string) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	id, ok := s.bindings[sevenProviderSessionKey{group, hash}]
	if !ok {
		return 0, service.ErrStickySessionNotFound
	}
	s.hits = append(s.hits, id)
	return id, nil
}

func (s *sevenProviderBindings) SetSessionAccountID(_ context.Context, group int64, hash string, id int64, _ time.Duration) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.bindings[sevenProviderSessionKey{group, hash}] = id
	return nil
}

func (s *sevenProviderBindings) DeleteSessionAccountID(_ context.Context, group int64, hash string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.bindings, sevenProviderSessionKey{group, hash})
	return nil
}

// Reuse the handler recovery fixture's generation/lease CAS and observe reads
// from the production scheduler on each independently constructed gateway.
type sevenProviderRouteStore struct {
	recoveryHandlerRouteStore
	observed []service.OpenAISessionRoute
}

func (s *sevenProviderRouteStore) Get(ctx context.Context, domain, digest string) (service.OpenAISessionRoute, error) {
	route, err := s.recoveryHandlerRouteStore.Get(ctx, domain, digest)
	if err == nil {
		s.mu.Lock()
		s.observed = append(s.observed, route)
		s.mu.Unlock()
	}
	return route, err
}

func (s *sevenProviderRouteStore) BeginMigration(ctx context.Context, domain, digest string, generation, candidate int64, lease, reason string, ttl time.Duration) (service.OpenAISessionRoute, error) {
	route, err := s.recoveryHandlerRouteStore.BeginMigration(ctx, domain, digest, generation, candidate, lease, reason, ttl)
	if err == nil {
		s.mu.Lock()
		s.route.Status = service.OpenAISessionRouteMigrating
		route = s.route
		s.mu.Unlock()
	}
	return route, err
}

func (s *sevenProviderRouteStore) CommitOwner(ctx context.Context, domain, digest, lease string, candidate int64) (service.OpenAISessionRoute, error) {
	route, err := s.recoveryHandlerRouteStore.CommitOwner(ctx, domain, digest, lease, candidate)
	if err == nil {
		s.mu.Lock()
		s.route.Status = service.OpenAISessionRouteActive
		s.route.LeaseExpiresAt = time.Time{}
		s.route.MigrationReason = ""
		route = s.route
		s.mu.Unlock()
	}
	return route, err
}

func (s *sevenProviderRouteStore) snapshot() *service.OpenAISessionRoute {
	s.mu.Lock()
	defer s.mu.Unlock()
	route := s.route
	return &route
}

func (s *sevenProviderRouteStore) MarkSuspended(_ context.Context, domain, digest string, generation int64, reason string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.route.AuthzDomain != domain || s.route.SessionDigest != digest || s.route.Generation != generation {
		return service.ErrOpenAISessionRouteLeaseLost
	}
	s.route.Status = service.OpenAISessionRouteSuspended
	s.route.CandidateAccountID, s.route.LeaseID = 0, ""
	s.route.LeaseExpiresAt = time.Time{}
	s.route.MigrationReason = reason
	return nil
}

func (s *sevenProviderRouteStore) assertCommittedOwner(t *testing.T, group, owner int64, secondGateway bool) {
	t.Helper()
	s.mu.Lock()
	defer s.mu.Unlock()
	require.Equal(t, fmt.Sprintf("group:%d", group), s.route.AuthzDomain)
	require.Len(t, s.route.SessionDigest, 64, "canonical storage holds an irreversible session digest")
	require.Equal(t, owner, s.route.OwnerAccountID)
	require.Equal(t, int64(2), s.route.Generation, "A to B advances the canonical generation exactly once")
	require.Equal(t, service.OpenAISessionRouteActive, s.route.Status)
	require.Empty(t, s.route.LeaseID, "successful semantic acceptance releases the migration lease")
	require.Zero(t, s.route.CandidateAccountID)
	require.Equal(t, []int64{7841}, s.createdOwners, "the initial A owner is created once")
	require.Equal(t, 1, s.migrations, "recovered A cannot acquire another migration")
	if secondGateway {
		require.NotEmpty(t, s.observed, "the second gateway must read the authoritative route")
		for _, route := range s.observed {
			require.Equal(t, owner, route.OwnerAccountID)
			require.Equal(t, int64(2), route.Generation)
			require.Equal(t, s.route.AuthzDomain, route.AuthzDomain)
			require.Equal(t, s.route.SessionDigest, route.SessionDigest)
		}
	}
	s.observed = nil
}

type sevenProviderBalanceCache struct {
	*handlerInflightCache
}

type sevenProviderSlotOwner struct {
	id      int64
	request string
}

// The shared counter fixture checks lifecycle balance; this local wrapper also
// checks that each release names the credential/user that acquired that slot.
type sevenProviderSlots struct {
	*grokMediaSlotsCache
	pairsMu                            sync.Mutex
	accountAcquired, accountReleased   []sevenProviderSlotOwner
	userSlotAcquired, userSlotReleased []sevenProviderSlotOwner
	wrongOwnerRelease                  bool
}

func (s *sevenProviderSlots) AcquireAccountSlot(ctx context.Context, id int64, limit int, request string) (bool, error) {
	acquired, err := s.grokMediaSlotsCache.AcquireAccountSlot(ctx, id, limit, request)
	if acquired && err == nil {
		s.pairsMu.Lock()
		s.accountAcquired = append(s.accountAcquired, sevenProviderSlotOwner{id, request})
		s.pairsMu.Unlock()
	}
	return acquired, err
}

func (s *sevenProviderSlots) ReleaseAccountSlot(ctx context.Context, id int64, request string) error {
	s.grokMediaSlotsCache.mu.Lock()
	owner, exists := s.accounts[request]
	s.grokMediaSlotsCache.mu.Unlock()
	s.pairsMu.Lock()
	s.wrongOwnerRelease = s.wrongOwnerRelease || !exists || owner != id
	s.accountReleased = append(s.accountReleased, sevenProviderSlotOwner{id, request})
	s.pairsMu.Unlock()
	return s.grokMediaSlotsCache.ReleaseAccountSlot(ctx, id, request)
}

func (s *sevenProviderSlots) AcquireUserSlot(ctx context.Context, id int64, limit int, request string) (bool, error) {
	acquired, err := s.grokMediaSlotsCache.AcquireUserSlot(ctx, id, limit, request)
	if acquired && err == nil {
		s.pairsMu.Lock()
		s.userSlotAcquired = append(s.userSlotAcquired, sevenProviderSlotOwner{id, request})
		s.pairsMu.Unlock()
	}
	return acquired, err
}

func (s *sevenProviderSlots) ReleaseUserSlot(ctx context.Context, id int64, request string) error {
	s.grokMediaSlotsCache.mu.Lock()
	owner, exists := s.users[request]
	s.grokMediaSlotsCache.mu.Unlock()
	s.pairsMu.Lock()
	s.wrongOwnerRelease = s.wrongOwnerRelease || !exists || owner != id
	s.userSlotReleased = append(s.userSlotReleased, sevenProviderSlotOwner{id, request})
	s.pairsMu.Unlock()
	return s.grokMediaSlotsCache.ReleaseUserSlot(ctx, id, request)
}

func (s *sevenProviderSlots) assertOwnerReleases(t *testing.T) {
	t.Helper()
	s.pairsMu.Lock()
	defer s.pairsMu.Unlock()
	require.False(t, s.wrongOwnerRelease, "every release must name its actual slot owner")
	require.ElementsMatch(t, s.accountAcquired, s.accountReleased, "account and request must both match their acquisition")
	require.ElementsMatch(t, s.userSlotAcquired, s.userSlotReleased, "user and request must both match their acquisition")
	require.Len(t, s.accountAcquired, 3)
	require.Equal(t, int64(7841), s.accountAcquired[0].id)
	require.Equal(t, int64(7842), s.accountAcquired[1].id)
	require.Equal(t, int64(7842), s.accountAcquired[2].id)
	require.Len(t, s.userSlotAcquired, 2)
	for _, slot := range s.userSlotAcquired {
		require.Equal(t, int64(7871), slot.id)
		require.NotEmpty(t, slot.request)
	}
}

func (s *sevenProviderBalanceCache) DeductUserBalance(_ context.Context, _ int64, amount float64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.balance -= amount
	return nil
}

type sevenProviderBillingRepo struct {
	service.UsageBillingRepository
	mu       sync.Mutex
	commands []*service.UsageBillingCommand
}

func (s *sevenProviderBillingRepo) Apply(_ context.Context, command *service.UsageBillingCommand) (*service.UsageBillingApplyResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.commands = append(s.commands, command)
	return &service.UsageBillingApplyResult{Applied: true, UsageLogPersisted: true}, nil
}

type sevenProviderWireRequest struct {
	method, host, path, authorization, proxyAuthorization string
	headers                                               http.Header
	canonicalRoute                                        *service.OpenAISessionRoute
	body                                                  []byte
}

type sevenProviderWireProxy struct {
	mu           sync.Mutex
	status       int
	errorBody    string
	observeRoute func() *service.OpenAISessionRoute
	requests     []sevenProviderWireRequest
	server       *httptest.Server
}

func newSevenProviderWireProxy(t *testing.T, status int) *sevenProviderWireProxy {
	t.Helper()
	proxy := &sevenProviderWireProxy{status: status}
	proxy.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "read request failed", http.StatusBadRequest)
			return
		}
		proxy.mu.Lock()
		var canonicalRoute *service.OpenAISessionRoute
		if proxy.observeRoute != nil {
			canonicalRoute = proxy.observeRoute()
		}
		proxy.requests = append(proxy.requests, sevenProviderWireRequest{
			method: r.Method, host: r.URL.Host, path: r.URL.Path,
			authorization: r.Header.Get("Authorization"), proxyAuthorization: r.Header.Get("Proxy-Authorization"),
			headers: r.Header.Clone(), body: body, canonicalRoute: canonicalRoute,
		})
		status, errorBody := proxy.status, proxy.errorBody
		requestNumber := len(proxy.requests)
		proxy.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Connection", "close")
		w.Header().Set("X-Request-ID", fmt.Sprintf("seven-provider-success-%d", requestNumber))
		if status != http.StatusOK {
			w.WriteHeader(status)
			if errorBody == "" {
				errorBody = `{"error":{"type":"upstream_unavailable","message":"temporarily unavailable"}}`
			}
			_, _ = io.WriteString(w, errorBody)
			return
		}
		_, _ = io.WriteString(w, `{"id":"chat-business","object":"chat.completion","model":"business-chat","choices":[{"index":0,"message":{"role":"assistant","content":"business success"},"finish_reason":"stop"}],"usage":{"prompt_tokens":10,"completion_tokens":5,"total_tokens":15}}`)
	}))
	t.Cleanup(proxy.server.Close)
	return proxy
}

func (s *sevenProviderWireProxy) accountProxy(t *testing.T, id int64, owner string) *service.Proxy {
	t.Helper()
	parsed, err := url.Parse(s.server.URL)
	require.NoError(t, err)
	port, err := strconv.Atoi(parsed.Port())
	require.NoError(t, err)
	return &service.Proxy{ID: id, Protocol: "http", Host: parsed.Hostname(), Port: port,
		Username: "proxy-" + owner, Password: "password-" + owner, Status: service.StatusActive}
}

func (s *sevenProviderWireProxy) snapshot() []sevenProviderWireRequest {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]sevenProviderWireRequest(nil), s.requests...)
}

// Each case enters the public handler, uses the production scheduler and HTTP
// transport, fails account A over to B, then follows B's persisted binding on a
// new gateway instance after A recovers. OpenAI uses the canonical route CAS;
// other providers retain their existing affinity store. Billing assertions are at the atomic
// command boundary; this fixture does not simulate PostgreSQL balance writes.
func TestSevenProviderChatFailoverPreservesWireOwnerStickySessionAndBilling(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, env := range []string{"XAI_GROK_CLI_VERSION", "SUB2API_DEEPSEEK_HARNESS_VERSION", "SUB2API_KIMI_CODE_VERSION", "SUB2API_ZCODE_VERSION"} {
		t.Setenv(env, "")
	}
	providers := []struct {
		platform, userAgent string
		companions          map[string]string
	}{
		{service.PlatformOpenAI, "codex_cli_rs/0.164.0 (Ubuntu 24.04; x86_64) xterm-256color", nil},
		{service.PlatformGrok, "grok-shell/1.0.45 (linux; x86_64)", map[string]string{"X-Grok-Client-Identifier": "grok-shell", "X-Grok-Client-Version": "1.0.45"}},
		{service.PlatformDeepseek, "deepseek-harness/0.2.1-rc.3 (+https://github.com/deepseek-ai/deepseek-harness)", nil},
		{service.PlatformKimi, "kimi-code-cli/2.3.1", map[string]string{"X-Msh-Platform": "kimi_code_cli", "X-Msh-Version": "2.3.1", "X-Stainless-Package-Version": "6.34.0"}},
		{service.PlatformMiniMax, "Anthropic/JS 0.91.1", map[string]string{"X-Stainless-Package-Version": "0.91.1"}},
		{service.PlatformStepFun, "stepcode (linux 6.8.0-31-generic; x64)", map[string]string{"X-Step-Client": "stepcode", "X-Stainless-Package-Version": "6.40.0"}},
		{service.PlatformZhipu, "ZCode/3.16.2 ai/6.0.193 ai-sdk/provider-utils/4.0.39 runtime/node.js/22", map[string]string{"X-ZCode-App-Version": "3.16.2"}},
	}
	for _, provider := range providers {
		for _, failure := range []int{http.StatusServiceUnavailable, http.StatusTooManyRequests} {
			t.Run(fmt.Sprintf("%s/%d", provider.platform, failure), func(t *testing.T) {
				var directDials atomic.Int64
				destination := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
					http.Error(w, "proxy bypass", http.StatusBadGateway)
				}))
				destination.Config.ConnState = func(_ net.Conn, state http.ConnState) {
					if state == http.StateNew {
						directDials.Add(1)
					}
				}
				destination.Start()
				t.Cleanup(destination.Close)
				proxyA := newSevenProviderWireProxy(t, failure)
				proxyB := newSevenProviderWireProxy(t, http.StatusOK)
				if provider.platform == service.PlatformOpenAI && failure == http.StatusTooManyRequests {
					// Canonical API-key sessions recover transient throttling on
					// the same owner; terminal account quota authorizes takeover.
					proxyA.mu.Lock()
					proxyA.errorBody = `{"error":{"type":"insufficient_quota","code":"insufficient_quota","message":"Account quota exhausted"}}`
					proxyA.mu.Unlock()
				}
				groupID := int64(7840)
				accounts := []service.Account{
					{ID: 7841, Name: "business-a", Platform: provider.platform, Type: service.AccountTypeAPIKey,
						Status: service.StatusActive, Schedulable: true, Concurrency: 3, Priority: 0, GroupIDs: []int64{groupID},
						Credentials: map[string]any{"api_key": "business-key-a", "base_url": destination.URL, "api_protocol": "chat_completions", "model_mapping": map[string]any{"business-chat": "business-chat"}},
						Extra:       map[string]any{"openai_responses_supported": false}},
					{ID: 7842, Name: "business-b", Platform: provider.platform, Type: service.AccountTypeAPIKey,
						Status: service.StatusActive, Schedulable: true, Concurrency: 3, Priority: 10, GroupIDs: []int64{groupID},
						Credentials: map[string]any{"api_key": "business-key-b", "base_url": destination.URL, "api_protocol": "chat_completions", "model_mapping": map[string]any{"business-chat": "business-chat"}},
						Extra:       map[string]any{"openai_responses_supported": false}},
				}
				if provider.platform == service.PlatformZhipu {
					for i := range accounts {
						accounts[i].Credentials[service.SettingKeyOutboundIdentity] = service.OutboundIdentitySelection{
							Preset: zcode.Preset, Headers: map[string]string{zcode.HeaderDeviceMid: "7d918214-79ad-4057-bb2b-4ac6bd24a907"},
						}
					}
				}
				accounts[0].Proxy = proxyA.accountProxy(t, 7851, "a")
				accounts[0].ProxyID = &accounts[0].Proxy.ID
				accounts[1].Proxy = proxyB.accountProxy(t, 7852, "b")
				accounts[1].ProxyID = &accounts[1].Proxy.ID
				repo := &grokCredentialHandlerRepo{accounts: accounts}
				bindings := &sevenProviderBindings{bindings: map[sevenProviderSessionKey]int64{}}
				var canonicalStore *sevenProviderRouteStore
				var sessionRoutes service.OpenAISessionRouteStore
				if provider.platform == service.PlatformOpenAI {
					canonicalStore = &sevenProviderRouteStore{}
					sessionRoutes = canonicalStore
					proxyA.mu.Lock()
					proxyA.observeRoute = canonicalStore.snapshot
					proxyA.mu.Unlock()
					proxyB.mu.Lock()
					proxyB.observeRoute = canonicalStore.snapshot
					proxyB.mu.Unlock()
				}
				slots := &sevenProviderSlots{grokMediaSlotsCache: &grokMediaSlotsCache{accounts: map[string]int64{}, users: map[string]int64{}}}
				concurrency := service.NewConcurrencyService(slots)
				cfg := &config.Config{}
				cfg.Gateway.MaxAccountSwitches = 2
				cfg.Gateway.Scheduling.LoadBatchEnabled = false
				cfg.Security.URLAllowlist.Enabled = false
				cfg.Security.URLAllowlist.AllowPrivateHosts = true
				cfg.Security.URLAllowlist.AllowInsecureHTTP = true
				cfg.Default.RateMultiplier = 1
				billingCache := service.NewBillingCacheService(&sevenProviderBalanceCache{newHandlerInflightCache(100)}, nil, nil, nil, nil, nil, cfg, nil)
				t.Cleanup(billingCache.Stop)
				billingRepo := &sevenProviderBillingRepo{}
				inputPrice, outputPrice := 0.000002, 0.000004
				channelService := service.NewChannelService(&openAIWSUsageHandlerChannelRepoStub{
					groupPlatforms: map[int64]string{groupID: provider.platform},
					channels: []service.Channel{{ID: 7860, Status: service.StatusActive, GroupIDs: []int64{groupID}, ModelPricing: []service.ChannelModelPricing{{
						Platform: provider.platform, Models: []string{"business-chat"}, BillingMode: service.BillingModeToken,
						InputPrice: &inputPrice, OutputPrice: &outputPrice,
					}}}},
				}, nil, nil, nil, nil)
				billingService := service.NewBillingService(cfg, nil)
				upstream := repository.NewHTTPUpstream(cfg)
				group := &service.Group{ID: groupID, Hydrated: true, Platform: provider.platform, Status: service.StatusActive, RateMultiplier: 1.5}
				apiKey := &service.APIKey{ID: 7870, UserID: 7871, GroupID: &groupID, Group: group, Quota: 100,
					User: &service.User{ID: 7871, Status: service.StatusActive, Balance: 100}}
				newRouter := func() *gin.Engine {
					gateway := service.NewOpenAIGatewayService(repo, nil, billingRepo, nil, nil, nil, bindings, cfg, nil, concurrency,
						billingService, nil, billingCache, upstream, &service.DeferredService{}, nil, nil,
						service.NewModelPricingResolver(channelService, billingService), channelService, nil, nil, nil, sessionRoutes)
					handler := NewOpenAIGatewayHandler(gateway, concurrency, billingCache, &service.APIKeyService{}, nil, nil, nil, nil, cfg)
					attachDisabledIPAccessControlForWebSocketTest(handler)
					router := gin.New()
					router.Use(func(c *gin.Context) {
						c.Set(string(middleware.ContextKeyAPIKey), apiKey)
						c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: apiKey.User.ID, Concurrency: 2})
						c.Next()
					})
					router.POST("/v1/chat/completions", handler.ChatCompletions)
					return router
				}
				request := func(router *gin.Engine, turn int) {
					ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
					defer cancel()
					body := fmt.Sprintf(`{"model":"business-chat","messages":[{"role":"user","content":"business turn %d"}]}`, turn)
					req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body)).WithContext(ctx)
					req.Header.Set("Content-Type", "application/json")
					req.Header.Set("session_id", "seven-provider-business-session")
					req.Header.Set("User-Agent", "untrusted-browser/99")
					req.Header.Set("Originator", "untrusted-origin")
					req.Header.Set("Version", "99.99.99")
					req.Header.Set("X-Stainless-Package-Version", "99.99.99")
					rec := httptest.NewRecorder()
					router.ServeHTTP(rec, req)
					require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
					require.Contains(t, rec.Body.String(), "business success")
				}

				request(newRouter(), 1)
				require.Len(t, proxyA.snapshot(), 1, "first account must actually fail on the wire")
				require.Len(t, proxyB.snapshot(), 1, "fallback account must actually succeed on the wire")
				if canonicalStore != nil {
					canonicalStore.assertCommittedOwner(t, groupID, 7842, false)
				} else {
					bindings.mu.Lock()
					require.NotEmpty(t, bindings.bindings, "successful fallback must persist the session owner")
					for _, owner := range bindings.bindings {
						require.Equal(t, int64(7842), owner)
					}
					bindings.hits = nil
					bindings.mu.Unlock()
				}
				slots.assertReleased(t)

				// Recover A before the next request. A new gateway shares durable
				// state while having no first-request runtime cooldown to hide an
				// incorrectly ignored session binding.
				repo.mu.Lock()
				for i := range repo.accounts {
					repo.accounts[i].Status = service.StatusActive
					repo.accounts[i].Schedulable = true
					repo.accounts[i].TempUnschedulableUntil = nil
					repo.accounts[i].RateLimitedAt = nil
					repo.accounts[i].RateLimitResetAt = nil
				}
				repo.mu.Unlock()
				proxyA.mu.Lock()
				proxyA.status = http.StatusOK
				proxyA.mu.Unlock()
				request(newRouter(), 2)
				require.Len(t, proxyA.snapshot(), 1, "recovered higher-priority A must not steal B's session")
				require.Len(t, proxyB.snapshot(), 2)
				if canonicalStore != nil {
					canonicalStore.assertCommittedOwner(t, groupID, 7842, true)
				} else {
					bindings.mu.Lock()
					require.Contains(t, bindings.hits, int64(7842), "second gateway must read B's persisted binding")
					bindings.mu.Unlock()
				}
				require.Zero(t, directDials.Load(), "neither credential owner may bypass its proxy")
				slots.assertReleased(t)
				slots.assertOwnerReleases(t)
				slots.mu.Lock()
				require.Equal(t, 3, slots.acquired, "A+B for the first request; B for the second")
				require.Equal(t, 2, slots.userAcquired, "one user slot per inbound request")
				slots.mu.Unlock()

				logical, err := url.Parse(destination.URL)
				require.NoError(t, err)
				for index, proxy := range []*sevenProviderWireProxy{proxyA, proxyB} {
					owner := []string{"a", "b"}[index]
					for attempt, wire := range proxy.snapshot() {
						if canonicalStore != nil {
							require.NotNil(t, wire.canonicalRoute)
							require.Equal(t, fmt.Sprintf("group:%d", groupID), wire.canonicalRoute.AuthzDomain)
							if index == 0 || attempt == 0 {
								require.Equal(t, int64(7841), wire.canonicalRoute.OwnerAccountID, "B cannot become canonical before its response is accepted")
								require.Equal(t, int64(1), wire.canonicalRoute.Generation)
								if index == 1 {
									require.Equal(t, int64(7842), wire.canonicalRoute.CandidateAccountID)
									require.NotEmpty(t, wire.canonicalRoute.LeaseID, "takeover must acquire its admission lease before writing upstream")
									require.Equal(t, service.OpenAISessionRouteMigrating, wire.canonicalRoute.Status)
								}
							} else {
								require.Equal(t, int64(7842), wire.canonicalRoute.OwnerAccountID)
								require.Equal(t, int64(2), wire.canonicalRoute.Generation)
								require.Empty(t, wire.canonicalRoute.LeaseID)
							}
						}
						require.Equal(t, http.MethodPost, wire.method)
						require.Equal(t, logical.Host, wire.host)
						require.Equal(t, "/v1/chat/completions", wire.path)
						require.Equal(t, "Bearer business-key-"+owner, wire.authorization)
						require.Equal(t, "Basic "+base64.StdEncoding.EncodeToString([]byte("proxy-"+owner+":password-"+owner)), wire.proxyAuthorization)
						require.Equal(t, provider.userAgent, wire.headers.Get("User-Agent"))
						require.NotEqual(t, "untrusted-origin", wire.headers.Get("Originator"))
						require.NotEqual(t, "99.99.99", wire.headers.Get("Version"))
						for header, expected := range provider.companions {
							require.Equal(t, expected, wire.headers.Get(header), header)
						}
						var payload map[string]any
						require.NoError(t, json.Unmarshal(wire.body, &payload))
						require.Equal(t, "business-chat", payload["model"])
						require.NotEmpty(t, payload["messages"])
					}
				}

				billingRepo.mu.Lock()
				defer billingRepo.mu.Unlock()
				require.Len(t, billingRepo.commands, 2, "only two successful B responses create billing commands")
				for _, command := range billingRepo.commands {
					require.NotEmpty(t, command.RequestID)
					require.Equal(t, int64(7842), command.AccountID, "failed A must never be charged")
					require.Equal(t, apiKey.ID, command.APIKeyID)
					require.Equal(t, apiKey.User.ID, command.UserID)
					require.Equal(t, "business-chat", command.Model)
					require.Equal(t, 10, command.InputTokens)
					require.Equal(t, 5, command.OutputTokens)
					// Business rates: $2/million input, $4/million output,
					// 10 input + 5 output tokens, and group multiplier 1.5.
					require.InDelta(t, 0.00006000, command.BalanceCost, 1e-12)
					require.InDelta(t, 0.00006000, command.APIKeyQuotaCost, 1e-12)
					require.Zero(t, command.SubscriptionCost)
					require.NotNil(t, command.UsageLog)
					require.Equal(t, int64(7842), command.UsageLog.AccountID)
					require.InDelta(t, 0.00004000, command.UsageLog.TotalCost, 1e-12)
					require.InDelta(t, 0.00006000, command.UsageLog.ActualCost, 1e-12)
				}
				require.NotEqual(t, billingRepo.commands[0].RequestID, billingRepo.commands[1].RequestID)
			})
		}
	}
}
