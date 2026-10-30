//go:build unit

package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/config"
	"github.com/LuckyKuang/sub2api-plus/internal/securityaudit"
	middleware2 "github.com/LuckyKuang/sub2api-plus/internal/server/middleware"
	"github.com/LuckyKuang/sub2api-plus/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type codexCLIIngressAudits struct {
	mu                       sync.Mutex
	blockLegacy, blockPrompt bool
	legacy, prompt           []securityaudit.Request
	legacyTask, promptTask   []bool
}

func (a *codexCLIIngressAudits) EffectiveMode() securityaudit.Mode                    { return securityaudit.ModeBlocking }
func (a *codexCLIIngressAudits) Enqueue(context.Context, securityaudit.Request) error { return nil }
func (a *codexCLIIngressAudits) Check(ctx context.Context, request securityaudit.Request) (*securityaudit.LegacyDecision, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.legacy, a.legacyTask = append(a.legacy, request.Clone()), append(a.legacyTask, service.IsCodexCLITask(ctx))
	return &securityaudit.LegacyDecision{Allowed: !a.blockLegacy, Blocked: a.blockLegacy, StatusCode: http.StatusForbidden, ErrorCode: "fixture_legacy_block", Message: "blocked"}, nil
}
func (a *codexCLIIngressAudits) Evaluate(ctx context.Context, request securityaudit.Request) (*securityaudit.PromptDecision, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.prompt, a.promptTask = append(a.prompt, request.Clone()), append(a.promptTask, service.IsCodexCLITask(ctx))
	kind := securityaudit.DecisionAllow
	if a.blockPrompt {
		kind = securityaudit.DecisionBlock
	}
	return &securityaudit.PromptDecision{Kind: kind, AllowNextStage: !a.blockPrompt}, nil
}

type codexCLIIngressBoundary struct {
	repositoryReads  atomic.Int32
	billingReads     atomic.Int32
	userSlots        atomic.Int32
	accountSlots     atomic.Int32
	slots            concurrencyCacheMock
	wire             openAIClientProfileFallbackUpstream
	mu               sync.Mutex
	acquiredAccounts []int64
}

type codexCLIIngressRepository struct {
	openAIWSFailoverHandlerAccountRepoStub
	boundary *codexCLIIngressBoundary
}

func (r *codexCLIIngressRepository) ListSchedulableByPlatform(ctx context.Context, platform string) ([]service.Account, error) {
	r.boundary.repositoryReads.Add(1)
	return r.openAIWSFailoverHandlerAccountRepoStub.ListSchedulableByPlatform(ctx, platform)
}
func (r *codexCLIIngressRepository) ListSchedulableByGroupIDAndPlatform(ctx context.Context, _ int64, platform string) ([]service.Account, error) {
	return r.ListSchedulableByPlatform(ctx, platform)
}
func (r *codexCLIIngressRepository) ListSchedulableUngroupedByPlatform(ctx context.Context, platform string) ([]service.Account, error) {
	return r.ListSchedulableByPlatform(ctx, platform)
}

type codexCLIIngressBilling struct {
	service.BillingCache
	boundary *codexCLIIngressBoundary
}

func (b *codexCLIIngressBilling) GetUserBalance(context.Context, int64) (float64, error) {
	b.boundary.billingReads.Add(1)
	return 100, nil
}
func (b *codexCLIIngressBilling) GetUserPlatformQuotaCache(context.Context, int64, string) (*service.UserPlatformQuotaCacheEntry, bool, error) {
	return nil, false, nil
}

func newCodexCLIIngressHandler(t *testing.T, coordinator *securityaudit.Coordinator, accounts []service.Account, simple bool) (*OpenAIGatewayHandler, *codexCLIIngressBoundary) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	boundary := &codexCLIIngressBoundary{}
	boundary.slots.acquireUserSlotFn = func(context.Context, int64, int, string) (bool, error) { boundary.userSlots.Add(1); return true, nil }
	boundary.slots.acquireAccountSlotFn = func(_ context.Context, id int64, _ int, _ string) (bool, error) {
		boundary.accountSlots.Add(1)
		boundary.mu.Lock()
		boundary.acquiredAccounts = append(boundary.acquiredAccounts, id)
		boundary.mu.Unlock()
		return true, nil
	}
	cfg := &config.Config{RunMode: config.RunModeStandard}
	if simple {
		cfg.RunMode = config.RunModeSimple
	}
	cfg.Default.RateMultiplier = 1
	cfg.Security.URLAllowlist.Enabled = false
	concurrency := service.NewConcurrencyService(&boundary.slots)
	billing := service.NewBillingCacheService(&codexCLIIngressBilling{boundary: boundary}, nil, nil, nil, nil, nil, cfg, nil)
	t.Cleanup(billing.Stop)
	repository := &codexCLIIngressRepository{openAIWSFailoverHandlerAccountRepoStub: openAIWSFailoverHandlerAccountRepoStub{accounts: accounts}, boundary: boundary}
	gateway := service.NewOpenAIGatewayService(repository, nil, nil, nil, nil, nil, nil, cfg, nil, concurrency,
		service.NewBillingService(cfg, nil), nil, billing, &boundary.wire, &service.DeferredService{}, nil, nil, nil, nil, nil, nil, nil, nil)
	h := NewOpenAIGatewayHandler(gateway, concurrency, billing, &service.APIKeyService{}, nil, nil, nil, nil, cfg)
	h.securityAuditCoordinator = coordinator
	return h, boundary
}

func codexCLIIngressContext(body string, authenticated bool) (*gin.Context, *httptest.ResponseRecorder) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/codex-cli/responses", strings.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	if authenticated {
		groupID := int64(1827)
		user := &service.User{ID: 1828, Status: service.StatusActive}
		c.Set(string(middleware2.ContextKeyAPIKey), &service.APIKey{ID: 1829, UserID: user.ID, User: user, GroupID: &groupID, Group: &service.Group{ID: groupID, Platform: service.PlatformOpenAI, Status: service.StatusActive, RateMultiplier: 1}})
		c.Set(string(middleware2.ContextKeyUser), middleware2.AuthSubject{UserID: user.ID, Concurrency: 1})
	}
	return c, w
}

func assertCodexCLIIngressNoSideEffects(t *testing.T, boundary *codexCLIIngressBoundary) {
	t.Helper()
	require.Zero(t, boundary.repositoryReads.Load(), "request must not select an account")
	require.Zero(t, boundary.billingReads.Load(), "request must not check or change account billing")
	require.Zero(t, boundary.userSlots.Load(), "request must not acquire user concurrency")
	require.Zero(t, boundary.accountSlots.Load(), "request must not acquire account concurrency")
	require.Empty(t, boundary.wire.calls(), "request must never reach native upstream transport")
}

func TestCodexCLIResponsesBothAuditEnginesSeeOriginalContentBeforeSideEffects(t *testing.T) {
	for _, blocking := range []string{"legacy", "prompt"} {
		t.Run(blocking, func(t *testing.T) {
			audits := &codexCLIIngressAudits{blockLegacy: blocking == "legacy", blockPrompt: blocking == "prompt"}
			h, boundary := newCodexCLIIngressHandler(t, securityaudit.NewCoordinator(audits, audits), nil, false)
			body := "{\n  \"model\":\"gpt-5.1\",\"instructions\":\"original developer instructions\",\"input\":\"original user input\",\"stream\":true,\"reasoning\":{\"effort\":\"high\"}\n}"
			c, w := codexCLIIngressContext(body, true)
			h.CodexCLIResponses(c)
			require.Equal(t, http.StatusForbidden, w.Code, w.Body.String())
			require.NotContains(t, w.Body.String(), "data:", "audit rejection must preserve an HTTP error before streaming starts")
			audits.mu.Lock()
			defer audits.mu.Unlock()
			require.Len(t, audits.legacy, 1)
			require.Len(t, audits.prompt, 1)
			require.Equal(t, []bool{true}, audits.legacyTask)
			require.Equal(t, []bool{true}, audits.promptTask)
			for _, request := range []securityaudit.Request{audits.legacy[0], audits.prompt[0]} {
				require.Equal(t, body, string(request.Body), "audit receives unchanged ingress bytes, including instructions and input")
				require.Equal(t, service.ContentModerationProtocolOpenAIResponses, request.Protocol)
				require.Equal(t, "gpt-5.1", request.Model)
			}
			assertCodexCLIIngressNoSideEffects(t, boundary)
		})
	}
}

func TestCodexCLIResponsesRealAuditEnginesPreserveCanonicalUserSelection(t *testing.T) {
	for _, engine := range []string{"moderation", "prompt"} {
		t.Run(engine, func(t *testing.T) {
			coordinator, supplier := newCountTokensAuditCoordinator(t, engine, true, 1)
			h, boundary := newCodexCLIIngressHandler(t, coordinator, nil, false)
			c, w := codexCLIIngressContext(`{"model":"gpt-5.1","instructions":"audit original instructions","input":"audit original input"}`, true)
			h.CodexCLIResponses(c)
			require.Equal(t, http.StatusForbidden, w.Code, w.Body.String())
			scans := supplier.snapshot()
			require.Len(t, scans, 1)
			require.NotContains(t, scans[0], "audit original instructions", "instructions remain context under the canonical audit contract")
			require.Contains(t, scans[0], "audit original input")
			assertCodexCLIIngressNoSideEffects(t, boundary)
		})
	}
}

func TestCodexCLIResponsesUnsupportedShapesFailBeforeAuditAndSideEffects(t *testing.T) {
	for _, body := range []string{
		`{"model":"gpt-5.1","input":[{"role":"user","content":"message"}]}`,
		`{"model":"gpt-5.1","input":[{"type":"input_image","image_url":"https://image.invalid"}]}`,
		`{"model":"gpt-5.1","input":"task","tools":[]}`,
		`{"model":"gpt-5.1","input":"task","previous_response_id":"resp_other"}`,
		`{"model":"gpt-5.1","input":"task","conversation":"conversation_other"}`,
		`{"model":"gpt-5.1","input":"task","future_option":{"opaque":true}}`,
		`{"model":"gpt-5.1","input":"task","max_output_tokens":12}`,
		`{"model":"gpt-5.1","input":"task","temperature":0.1}`,
		`{"model":"gpt-5.1","input":"task","instructions":{}}`,
		`{"model":"gpt-5.1","input":"task","reasoning":{"effort":"high","summary":"auto"}}`,
		`{"model":"gpt-5.1","input":"task","stream":null}`,
		`{"model":"gpt-5.1","input":""}`,
		`{"model":"gpt-5.1","input":null}`,
		`{"input":"task"}`,
		`{"model":"gpt-5.1","input":"task"`,
	} {
		t.Run(body, func(t *testing.T) {
			audits := &codexCLIIngressAudits{blockPrompt: true}
			h, boundary := newCodexCLIIngressHandler(t, securityaudit.NewCoordinator(audits, audits), nil, false)
			c, w := codexCLIIngressContext(body, true)
			h.CodexCLIResponses(c)
			require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
			audits.mu.Lock()
			require.Empty(t, audits.legacy)
			require.Empty(t, audits.prompt)
			audits.mu.Unlock()
			assertCodexCLIIngressNoSideEffects(t, boundary)
		})
	}
}

func TestCodexCLIResponsesRequiresAuthenticationBeforeAudit(t *testing.T) {
	audits := &codexCLIIngressAudits{blockPrompt: true}
	h, boundary := newCodexCLIIngressHandler(t, securityaudit.NewCoordinator(audits, audits), nil, false)
	c, w := codexCLIIngressContext(`{"model":"gpt-5.1","input":"task"}`, false)
	h.CodexCLIResponses(c)
	require.Equal(t, http.StatusUnauthorized, w.Code, w.Body.String())
	require.Empty(t, audits.legacy)
	require.Empty(t, audits.prompt)
	assertCodexCLIIngressNoSideEffects(t, boundary)
}

func TestOrdinaryResponsesDoesNotEnableCLITransportFromHeadersOrRequestPath(t *testing.T) {
	audits := &codexCLIIngressAudits{blockPrompt: true}
	h, boundary := newCodexCLIIngressHandler(t, securityaudit.NewCoordinator(audits, audits), nil, false)
	c, w := codexCLIIngressContext(`{"model":"gpt-5.1","instructions":"ordinary instructions","input":"ordinary input","future_option":true}`, true)
	c.Request.Header.Set("X-Codex-CLI", "true")
	c.Request.Header.Set("User-Agent", "codex_cli_rs/0.165.0")
	h.Responses(c)
	require.Equal(t, http.StatusForbidden, w.Code, w.Body.String())
	require.False(t, service.IsCodexCLITask(c.Request.Context()), "only the explicit route wrapper can authorize task mode")
	require.Equal(t, []bool{false}, audits.legacyTask)
	require.Equal(t, []bool{false}, audits.promptTask)
	assertCodexCLIIngressNoSideEffects(t, boundary)
}

func TestCodexCLIResponsesRejectsDisabledAccountsAndNeverFallsBackToNativeTransport(t *testing.T) {
	accounts := []service.Account{
		{ID: 1831, Name: "not enabled", Platform: service.PlatformOpenAI, Type: service.AccountTypeOAuth, Status: service.StatusActive, Schedulable: true, Concurrency: 1, Priority: 1, Credentials: map[string]any{"access_token": "fixture-one"}},
		{ID: 1832, Name: "explicitly disabled", Platform: service.PlatformOpenAI, Type: service.AccountTypeOAuth, Status: service.StatusActive, Schedulable: true, Concurrency: 1, Priority: 2, Credentials: map[string]any{"access_token": "fixture-two", service.CodexCLIConfigCredentialKey: map[string]any{"enabled": false, "config_toml": service.DefaultCodexCLIConfigTOML}}},
		{ID: 1833, Name: "wrong account type", Platform: service.PlatformOpenAI, Type: service.AccountTypeAPIKey, Status: service.StatusActive, Schedulable: true, Concurrency: 1, Priority: 3, Credentials: map[string]any{"api_key": "fixture-three", "base_url": "https://upstream.invalid", service.CodexCLIConfigCredentialKey: map[string]any{"enabled": true, "config_toml": service.DefaultCodexCLIConfigTOML}}},
	}
	audits := &codexCLIIngressAudits{}
	h, boundary := newCodexCLIIngressHandler(t, securityaudit.NewCoordinator(audits, audits), accounts, true)
	c, w := codexCLIIngressContext(`{"model":"gpt-5.1","input":"task"}`, true)
	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()
	c.Request = c.Request.WithContext(ctx)
	h.CodexCLIResponses(c)
	require.Equal(t, http.StatusServiceUnavailable, w.Code, w.Body.String())
	require.Positive(t, boundary.repositoryReads.Load())
	require.Equal(t, int32(1), boundary.userSlots.Load())
	require.Equal(t, int32(3), boundary.accountSlots.Load(), "the scheduler must actually select all three ineligible accounts for this regression")
	boundary.mu.Lock()
	require.ElementsMatch(t, []int64{1831, 1832, 1833}, boundary.acquiredAccounts)
	boundary.mu.Unlock()
	require.Equal(t, int32(3), atomic.LoadInt32(&boundary.slots.releaseAccountCalled), "each skipped selection releases its scheduler-owned lease")
	require.Equal(t, int32(1), atomic.LoadInt32(&boundary.slots.releaseUserCalled))
	require.Empty(t, boundary.wire.calls(), "CLI tasks must not be replayed through normal Responses transport when no account opted in")
}
