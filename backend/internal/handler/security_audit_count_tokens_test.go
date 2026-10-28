//go:build unit

package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	sqlmock "github.com/DATA-DOG/go-sqlmock"
	"github.com/LuckyKuang/sub2api-plus/internal/config"
	"github.com/LuckyKuang/sub2api-plus/internal/pkg/tlsfingerprint"
	"github.com/LuckyKuang/sub2api-plus/internal/securityaudit"
	"github.com/LuckyKuang/sub2api-plus/internal/server/middleware"
	"github.com/LuckyKuang/sub2api-plus/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

type countTokensAuditAccountRepo struct {
	openAIWSFailoverHandlerAccountRepoStub
	reads *atomic.Int32
}

func (r *countTokensAuditAccountRepo) ListSchedulableByPlatform(ctx context.Context, platform string) ([]service.Account, error) {
	r.reads.Add(1)
	return r.openAIWSFailoverHandlerAccountRepoStub.ListSchedulableByPlatform(ctx, platform)
}

func (r *countTokensAuditAccountRepo) ListSchedulableByGroupIDAndPlatform(ctx context.Context, _ int64, platform string) ([]service.Account, error) {
	return r.ListSchedulableByPlatform(ctx, platform)
}

func (r *countTokensAuditAccountRepo) ListSchedulableUngroupedByPlatform(ctx context.Context, platform string) ([]service.Account, error) {
	return r.ListSchedulableByPlatform(ctx, platform)
}

type countTokensAuditScheduler struct {
	fakeSchedulerCache
	reads *atomic.Int32
}

func (s *countTokensAuditScheduler) GetSnapshot(ctx context.Context, bucket service.SchedulerBucket) ([]*service.Account, bool, error) {
	s.reads.Add(1)
	return s.fakeSchedulerCache.GetSnapshot(ctx, bucket)
}

type countTokensAuditUpstream struct {
	openAIClientProfileFallbackUpstream
}

func (u *countTokensAuditUpstream) DoWithTLS(req *http.Request, proxy string, accountID int64, concurrency int, _ *tlsfingerprint.Profile) (*http.Response, error) {
	return u.Do(req, proxy, accountID, concurrency)
}

type countTokensAuditBoundary struct {
	reads        atomic.Int32
	wire         countTokensAuditUpstream
	slots        concurrencyCacheMock
	userSlots    atomic.Int32
	accountSlots atomic.Int32
}

func newCountTokensAuditHandler(t *testing.T, endpoint, accountType string, coordinator *securityaudit.Coordinator) (gin.HandlerFunc, *service.Group, *countTokensAuditBoundary) {
	t.Helper()
	boundary := &countTokensAuditBoundary{}
	boundary.wire.body = `{"input_tokens":42}`
	boundary.slots.acquireUserSlotFn = func(context.Context, int64, int, string) (bool, error) {
		boundary.userSlots.Add(1)
		return true, nil
	}
	boundary.slots.acquireAccountSlotFn = func(context.Context, int64, int, string) (bool, error) {
		boundary.accountSlots.Add(1)
		return true, nil
	}
	platform := service.PlatformOpenAI
	if endpoint == "anthropic" {
		platform = service.PlatformAnthropic
	} else if endpoint == "grok" {
		platform = service.PlatformGrok
	}
	group := &service.Group{ID: 8650, Platform: platform, Status: service.StatusActive, RateMultiplier: 1, AllowMessagesDispatch: true}
	account := service.Account{ID: 8651, Platform: platform, Type: accountType, Status: service.StatusActive, Schedulable: true, Concurrency: 1,
		GroupIDs: []int64{group.ID}, Credentials: map[string]any{"api_key": "fixture-api-key", "access_token": "fixture-oauth-token", "chatgpt_account_id": "fixture-principal", "base_url": "https://token-count.invalid"}}
	cfg := &config.Config{RunMode: config.RunModeSimple}
	cfg.Default.RateMultiplier = 1
	cfg.Security.URLAllowlist.Enabled = false
	billing := service.NewBillingCacheService(nil, nil, nil, nil, nil, nil, cfg, nil)
	t.Cleanup(billing.Stop)
	concurrency := service.NewConcurrencyService(&boundary.slots)
	if endpoint == "grok" {
		h := &OpenAIGatewayHandler{cfg: cfg, securityAuditCoordinator: coordinator,
			concurrencyHelper: NewConcurrencyHelper(concurrency, SSEPingFormatNone, 0)}
		return h.GrokCountTokens, group, boundary
	}
	if endpoint == "openai" {
		repo := &countTokensAuditAccountRepo{openAIWSFailoverHandlerAccountRepoStub: openAIWSFailoverHandlerAccountRepoStub{accounts: []service.Account{account}}, reads: &boundary.reads}
		gateway := service.NewOpenAIGatewayService(repo, nil, nil, nil, nil, nil, nil, cfg, nil, concurrency, service.NewBillingService(cfg, nil), nil, billing,
			&boundary.wire, &service.DeferredService{}, nil, nil, nil, nil, nil, nil, nil, nil)
		h := NewOpenAIGatewayHandler(gateway, concurrency, billing, &service.APIKeyService{}, nil, nil, nil, nil, cfg)
		h.securityAuditCoordinator = coordinator
		return h.CountTokens, group, boundary
	}
	scheduler := service.NewSchedulerSnapshotService(&countTokensAuditScheduler{fakeSchedulerCache: fakeSchedulerCache{accounts: []*service.Account{&account}}, reads: &boundary.reads}, nil, nil, nil, nil)
	gateway := service.NewGatewayService(nil, &fakeGroupRepo{group: group}, nil, nil, nil, nil, nil, nil, cfg, scheduler,
		nil, service.NewBillingService(cfg, nil), nil, billing, nil, &boundary.wire, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	h := &GatewayHandler{gatewayService: gateway, billingCacheService: billing, cfg: cfg, securityAuditCoordinator: coordinator,
		concurrencyHelper: NewConcurrencyHelper(concurrency, SSEPingFormatNone, 0)}
	return h.CountTokens, group, boundary
}

type countTokensAuditSupplier struct {
	mu     sync.Mutex
	bodies []string
}

func (s *countTokensAuditSupplier) snapshot() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.bodies...)
}

func newCountTokensAuditCoordinator(t *testing.T, engine string, block bool, expectedScans int) (*securityaudit.Coordinator, *countTokensAuditSupplier) {
	t.Helper()
	supplier := &countTokensAuditSupplier{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		supplier.mu.Lock()
		supplier.bodies = append(supplier.bodies, string(body))
		supplier.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		if engine == "moderation" {
			score := 0.0
			if block {
				score = 0.99
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"results": []any{map[string]any{"category_scores": map[string]float64{"sexual": score}}}})
			return
		}
		content := "Safety: Safe\nCategories: None"
		if block {
			content = "Safety: Unsafe\nCategories: Violent"
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]string{"content": content}}}})
	}))
	t.Cleanup(server.Close)
	if engine == "moderation" {
		cfg := &service.ContentModerationConfig{Enabled: true, Mode: service.ContentModerationModePreBlock, BaseURL: server.URL,
			Model: "omni-moderation-latest", APIKeys: []string{"moderation-fixture"}, SampleRate: 100, AllGroups: true}
		raw, err := json.Marshal(cfg)
		require.NoError(t, err)
		settings := &contentModerationHandlerSettingRepo{values: map[string]string{service.SettingKeyRiskControlEnabled: "true", service.SettingKeyContentModerationConfig: string(raw)}}
		moderation := service.NewContentModerationService(settings, &contentModerationHandlerTestRepo{}, nil, nil, nil, nil, nil, nil)
		return securityaudit.NewCoordinator(securityaudit.NewLegacyModerationAdapter(moderation), nil), supplier
	}
	db, persistence, err := sqlmock.New()
	require.NoError(t, err)
	if expectedScans > 0 {
		// Audit result recording is best effort. An unavailable persistence
		// transaction must preserve the supplier's policy decision.
		persistence.ExpectBegin().WillReturnError(errors.New("fixture audit persistence unavailable"))
	}
	persistence.ExpectClose()
	t.Cleanup(func() {
		require.NoError(t, db.Close())
		require.NoError(t, persistence.ExpectationsWereMet())
	})
	prompt := securityaudit.NewPromptService(blockingCompatibilityConfigStore{cfg: securityaudit.ActiveConfig{
		RiskControlEnabled: true, Enabled: true, BlockingEnabled: true, AllGroups: true, Scanners: []string{"violent"},
		Endpoints: []securityaudit.ActiveEndpoint{{ID: "token-count-guard", Enabled: true, Protocol: "openai", BaseURL: server.URL,
			Model: "qwen3guard", Token: "guard-fixture", TimeoutMS: 1000, InputLimit: 4096}},
	}}, securityaudit.NewPostgreSQLRepository(db), nil, securityaudit.NewOpenAICompatibleScanner(), securityaudit.NewAtomicMetrics())
	return securityaudit.NewCoordinator(nil, prompt), supplier
}

func serveCountTokensAuditRequest(t *testing.T, send gin.HandlerFunc, group *service.Group, body string) (int, []byte, bool) {
	t.Helper()
	var auditCompleted atomic.Bool
	router := gin.New()
	router.Use(func(c *gin.Context) {
		user := &service.User{ID: 8652, Status: service.StatusActive}
		c.Set(string(middleware.ContextKeyAPIKey), &service.APIKey{ID: 8653, UserID: user.ID, User: user, GroupID: &group.ID, Group: group})
		c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: user.ID, Concurrency: 1})
		c.Next()
	})
	router.POST("/v1/messages/count_tokens", func(c *gin.Context) {
		send(c)
		completed, _ := c.Get(securityAuditCompletedContextKey)
		auditCompleted.Store(completed == true)
	})
	server := httptest.NewServer(router)
	defer server.Close()
	response, err := server.Client().Post(server.URL+"/v1/messages/count_tokens", "application/json", strings.NewReader(body))
	require.NoError(t, err)
	defer func() { _ = response.Body.Close() }()
	raw, err := io.ReadAll(response.Body)
	require.NoError(t, err)
	return response.StatusCode, raw, auditCompleted.Load()
}

func TestCountTokensIngressAuditRealEnginesAndSideEffects(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, endpoint := range []string{"anthropic", "openai", "grok"} {
		accountTypes := []string{service.AccountTypeAPIKey, service.AccountTypeOAuth}
		if endpoint == "grok" {
			accountTypes = []string{"local_estimator"} // This handler selects no account.
		}
		for _, accountType := range accountTypes {
			for _, engine := range []string{"moderation", "prompt"} {
				for _, scenario := range []struct {
					name, content string
					block         bool
					status, scans int
				}{
					{"blocked_recognized_sibling", `[{"type":"text","text":"audit current request"},{"type":"future_block","payload":"OPAQUE_UNKNOWN_CANARY"}]`, true, http.StatusForbidden, 1},
					{"safe_recognized_sibling", `[{"type":"text","text":"audit current request"},{"type":"future_block","payload":"OPAQUE_UNKNOWN_CANARY"}]`, false, http.StatusOK, 1},
					{"unknown_only_passes", `[{"type":"future_block","payload":"OPAQUE_UNKNOWN_CANARY"}]`, true, http.StatusOK, 0},
				} {
					t.Run(endpoint+"/"+accountType+"/"+engine+"/"+scenario.name, func(t *testing.T) {
						coordinator, supplier := newCountTokensAuditCoordinator(t, engine, scenario.block, scenario.scans)
						send, group, boundary := newCountTokensAuditHandler(t, endpoint, accountType, coordinator)
						model := map[string]string{"anthropic": "claude-sonnet-4-6", "openai": "gpt-5.4", "grok": "grok-4"}[endpoint]
						body := fmt.Sprintf(`{"model":%q,"messages":[{"role":"user","content":%s}],"future_option":true}`, model, scenario.content)
						status, response, completed := serveCountTokensAuditRequest(t, send, group, body)
						require.Equal(t, scenario.status, status, string(response))
						scans := supplier.snapshot()
						require.Len(t, scans, scenario.scans)
						for _, scanned := range scans {
							require.Contains(t, scanned, "audit current request")
							require.NotContains(t, scanned, "OPAQUE_UNKNOWN_CANARY")
						}
						if scenario.status == http.StatusForbidden {
							require.Zero(t, boundary.reads.Load(), "blocked content must not select an account")
							require.Zero(t, boundary.userSlots.Load())
							require.Zero(t, boundary.accountSlots.Load())
							require.Empty(t, boundary.wire.calls(), "blocked content must not reach the provider")
							return
						}
						require.True(t, completed, "unknown valid content must enter and pass the audit gate")
						require.True(t, gjson.GetBytes(response, "input_tokens").Exists(), string(response))
						require.Equal(t, int32(1), boundary.userSlots.Load(), "all accepted count requests share global user admission")
						require.Equal(t, int32(1), atomic.LoadInt32(&boundary.slots.releaseUserCalled), "completion releases the global user lease")
						if endpoint == "grok" {
							require.Zero(t, boundary.reads.Load())
							require.Empty(t, boundary.wire.calls())
						} else {
							require.Positive(t, boundary.reads.Load())
							require.Equal(t, []int64{8651}, boundary.wire.calls(), "allowed content must complete the real forwarding path")
							require.Equal(t, int64(42), gjson.GetBytes(response, "input_tokens").Int())
						}
					})
				}
			}
		}
	}
}

func TestCountTokensIngressAuditsOriginalLongContextBody(t *testing.T) {
	for _, endpoint := range []string{"anthropic", "openai", "grok"} {
		t.Run(endpoint, func(t *testing.T) {
			engine := blockingHandlerPromptEngine()
			send, group, _ := newCountTokensAuditHandler(t, endpoint, service.AccountTypeAPIKey, securityaudit.NewCoordinator(nil, engine))
			const body = `{"model":"claude-sonnet-4-6[1m]","thinking":{"type":"enabled","budget_tokens":1024},"messages":[{"role":"user","content":"audit the original document"}]}`
			status, response, _ := serveCountTokensAuditRequest(t, send, group, body)
			require.Equal(t, http.StatusForbidden, status, string(response))
			evaluated, _, requests := engine.snapshot()
			require.Equal(t, 1, evaluated)
			require.Len(t, requests, 1)
			require.JSONEq(t, body, string(requests[0].Body), "parsing and long-context model normalization must not replace the audited document")
		})
	}
}

func TestGrokCountTokensRequiresAuthenticatedContext(t *testing.T) {
	for _, withAPIKey := range []bool{false, true} {
		t.Run(fmt.Sprintf("api_key_%t", withAPIKey), func(t *testing.T) {
			engine := blockingHandlerPromptEngine()
			h := &OpenAIGatewayHandler{securityAuditCoordinator: securityaudit.NewCoordinator(nil, engine)}
			router := gin.New()
			if withAPIKey {
				router.Use(func(c *gin.Context) {
					c.Set(string(middleware.ContextKeyAPIKey), &service.APIKey{ID: 1})
					c.Next()
				})
			}
			router.POST("/v1/messages/count_tokens", h.GrokCountTokens)
			request := httptest.NewRequest(http.MethodPost, "/v1/messages/count_tokens", strings.NewReader(`{"model":"grok-4","messages":[{"role":"user","content":"must authenticate first"}]}`))
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, request)
			expected := http.StatusUnauthorized
			if withAPIKey {
				expected = http.StatusInternalServerError
			}
			require.Equal(t, expected, recorder.Code)
			evaluated, _, _ := engine.snapshot()
			require.Zero(t, evaluated, "authentication and context validation must precede content audit")
		})
	}
}
