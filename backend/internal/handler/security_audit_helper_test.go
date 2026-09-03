package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/securityaudit"
	middleware2 "github.com/LuckyKuang/sub2api-plus/internal/server/middleware"
	"github.com/LuckyKuang/sub2api-plus/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

func TestCachesSecurityAuditCompletionSkipsWebSocketStages(t *testing.T) {
	require.True(t, cachesSecurityAuditCompletion("http"))
	require.True(t, cachesSecurityAuditCompletion(""))
	require.False(t, cachesSecurityAuditCompletion("first_turn"))
	require.False(t, cachesSecurityAuditCompletion("subsequent_turn"))
	require.True(t, isSecurityAuditWebSocketStage("first_turn"))
	require.True(t, isSecurityAuditWebSocketStage("subsequent_turn"))
	require.False(t, isSecurityAuditWebSocketStage("http"))
}

func TestBuildSecurityAuditRequestCapturesClientIP(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	c.Request.RemoteAddr = "203.0.113.42:4321"

	request := buildSecurityAuditRequest(c, nil, middleware2.AuthSubject{UserID: 7}, "openai_responses", "gpt-test", []byte(`{"input":"hi"}`), "http")
	require.Equal(t, "203.0.113.42", request.ClientIP)
}

func TestRunSecurityAuditDoesNotSkipSubsequentWebSocketTurns(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := &turnCountingEngine{mode: securityaudit.ModeAsync}
	coordinator := securityaudit.NewCoordinator(nil, engine)

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)

	subject := middleware2.AuthSubject{UserID: 7, Concurrency: 1}
	first := runSecurityAudit(c, nil, coordinator, nil, nil, subject, "openai_responses", "gpt-test",
		[]byte(`{"type":"response.create","response":{"input":"benign"}}`), "first_turn")
	require.NotNil(t, first)
	require.True(t, first.AllowNextStage)
	require.Equal(t, int64(1), engine.enqueues.Load())
	_, cached := c.Get(securityAuditCompletedContextKey)
	require.False(t, cached, "WebSocket stages must not set the HTTP completion cache")

	// Even if an HTTP path previously cached completion on this Context, WS turns
	// must still audit every response.create payload.
	c.Set(securityAuditCompletedContextKey, true)

	second := runSecurityAudit(c, nil, coordinator, nil, nil, subject, "openai_responses", "gpt-test",
		[]byte(`{"type":"response.create","response":{"input":"malicious follow-up"}}`), "subsequent_turn")
	require.NotNil(t, second)
	require.Equal(t, int64(2), engine.enqueues.Load(), "subsequent WebSocket turns must be audited again")
}

func TestRunSecurityAuditDeduplicatesRepeatedPayloadWithinWebSocketTurn(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := &turnCountingEngine{mode: securityaudit.ModeBlocking}
	coordinator := securityaudit.NewCoordinator(nil, engine)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	payload := []byte(`{"type":"response.create","response":{"input":"same turn"}}`)
	c.Set(securityAuditWSTurnContextKey, 2)
	first := runSecurityAudit(c, nil, coordinator, nil, nil, middleware2.AuthSubject{UserID: 7}, "openai_responses", "gpt-test", payload, "subsequent_turn")
	second := runSecurityAudit(c, nil, coordinator, nil, nil, middleware2.AuthSubject{UserID: 7}, "openai_responses", "gpt-test", payload, "subsequent_turn")
	require.NotNil(t, first)
	require.NotNil(t, second)
	require.True(t, first.AllowNextStage)
	require.True(t, second.AllowNextStage)
	require.Equal(t, int64(1), engine.evaluates.Load())

	// The cache holds only one successful same-turn result.
	entry, exists := c.Get(securityAuditWSDedupeContextKey)
	require.True(t, exists)
	require.IsType(t, securityAuditWSDedupeEntry{}, entry)

	c.Set(securityAuditWSTurnContextKey, 3)
	runSecurityAudit(c, nil, coordinator, nil, nil, middleware2.AuthSubject{UserID: 7}, "openai_responses", "gpt-test", payload, "subsequent_turn")
	require.Equal(t, int64(2), engine.evaluates.Load())
}

func TestRunSecurityAuditDoesNotCacheFailedWebSocketDecision(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := &turnCountingEngine{
		mode: securityaudit.ModeBlocking,
		decisions: []*securityaudit.PromptDecision{
			{Kind: securityaudit.DecisionUnavailable, AllowNextStage: false},
			{Kind: securityaudit.DecisionAllow, AllowNextStage: true},
		},
	}
	coordinator := securityaudit.NewCoordinator(nil, engine)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	c.Set(securityAuditWSTurnContextKey, 2)
	payload := []byte(`{"type":"response.create","response":{"input":"retry me"}}`)

	first := runSecurityAudit(c, nil, coordinator, nil, nil, middleware2.AuthSubject{UserID: 7}, "openai_responses", "gpt-test", payload, "subsequent_turn")
	_, cachedAfterFailure := c.Get(securityAuditWSDedupeContextKey)
	second := runSecurityAudit(c, nil, coordinator, nil, nil, middleware2.AuthSubject{UserID: 7}, "openai_responses", "gpt-test", payload, "subsequent_turn")

	require.False(t, first.AllowNextStage)
	require.False(t, cachedAfterFailure)
	require.True(t, second.AllowNextStage)
	require.Equal(t, int64(2), engine.evaluates.Load())
}

func TestRunSecurityAuditDoesNotCacheFlaggedWebSocketDecision(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := &turnCountingEngine{
		mode: securityaudit.ModeBlocking,
		decisions: []*securityaudit.PromptDecision{
			{Kind: securityaudit.DecisionFlag, AllowNextStage: true},
			{Kind: securityaudit.DecisionAllow, AllowNextStage: true},
		},
	}
	coordinator := securityaudit.NewCoordinator(nil, engine)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	c.Set(securityAuditWSTurnContextKey, 2)
	payload := []byte(`{"type":"response.create","response":{"input":"retry flagged"}}`)

	first := runSecurityAudit(c, nil, coordinator, nil, nil, middleware2.AuthSubject{UserID: 7}, "openai_responses", "gpt-test", payload, "subsequent_turn")
	_, cachedAfterFlag := c.Get(securityAuditWSDedupeContextKey)
	second := runSecurityAudit(c, nil, coordinator, nil, nil, middleware2.AuthSubject{UserID: 7}, "openai_responses", "gpt-test", payload, "subsequent_turn")

	require.Equal(t, securityaudit.DecisionFlag, first.Kind)
	require.True(t, first.AllowNextStage)
	require.False(t, cachedAfterFlag)
	require.Equal(t, securityaudit.DecisionAllow, second.Kind)
	require.Equal(t, int64(2), engine.evaluates.Load())
}

func TestRunSecurityAuditLogsWebSocketChecksAndCacheHits(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := &turnCountingEngine{mode: securityaudit.ModeBlocking}
	coordinator := securityaudit.NewCoordinator(nil, engine)
	core, logs := observer.New(zap.InfoLevel)
	reqLog := zap.New(core)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	c.Set(securityAuditWSTurnContextKey, 2)
	payload := []byte(`{"type":"response.create","response":{"input":"same turn"}}`)

	runSecurityAudit(c, reqLog, coordinator, nil, nil, middleware2.AuthSubject{UserID: 7}, "openai_responses", "gpt-test", payload, "subsequent_turn")
	runSecurityAudit(c, reqLog, coordinator, nil, nil, middleware2.AuthSubject{UserID: 7}, "openai_responses", "gpt-test", payload, "subsequent_turn")

	startLogs := logs.FilterMessage("security_audit.gateway_check_start").All()
	require.Len(t, startLogs, 1)
	require.Equal(t, false, startLogs[0].ContextMap()["cached"])

	doneLogs := logs.FilterMessage("security_audit.gateway_check_done").All()
	require.Len(t, doneLogs, 2)
	require.Equal(t, false, doneLogs[0].ContextMap()["cached"])
	require.Equal(t, true, doneLogs[1].ContextMap()["cached"])
	require.Equal(t, "allow", doneLogs[1].ContextMap()["decision"])
	require.Equal(t, "subsequent_turn", doneLogs[1].ContextMap()["stage"])
	require.Equal(t, int64(1), engine.evaluates.Load())
}

func TestRunSecurityAuditExcludesHarnessAcrossHTTPAndWebSocketStages(t *testing.T) {
	gin.SetMode(gin.TestMode)
	httpPayload := []byte(`{
		"instructions":"You are Codex. sandbox_permissions require_escalated jailbreak",
		"tools":[{"type":"function","name":"exec","description":"Run JavaScript in the sandbox"}],
		"input":[{"type":"message","role":"user","content":"hi"}]
	}`)
	wsPayload := []byte(`{
		"type":"response.create",
		"response":{
			"instructions":"You are Codex. sandbox_permissions require_escalated jailbreak",
			"tools":[{"type":"function","name":"exec","description":"Run JavaScript in the sandbox"}],
			"input":[{"type":"message","role":"user","content":"hi"}]
		}
	}`)
	tests := []struct {
		name    string
		mode    securityaudit.Mode
		stage   string
		payload []byte
	}{
		{name: "blocking HTTP", mode: securityaudit.ModeBlocking, stage: "http", payload: httpPayload},
		{name: "blocking websocket", mode: securityaudit.ModeBlocking, stage: "subsequent_turn", payload: wsPayload},
		{name: "async HTTP", mode: securityaudit.ModeAsync, stage: "http", payload: httpPayload},
		{name: "async websocket", mode: securityaudit.ModeAsync, stage: "first_turn", payload: wsPayload},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			engine := &turnCountingEngine{mode: test.mode, captureSnapshot: true}
			coordinator := securityaudit.NewCoordinator(nil, engine)
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
			groupID := int64(3)
			apiKey := &service.APIKey{ID: 9, UserID: 7, GroupID: &groupID, Group: &service.Group{ID: groupID}}

			decision := runSecurityAudit(
				c, nil, coordinator, nil, apiKey,
				middleware2.AuthSubject{UserID: 7},
				service.ContentModerationProtocolOpenAIResponses, "codex-auto-review", test.payload, test.stage,
			)

			require.NotNil(t, decision)
			require.True(t, decision.AllowNextStage)
			require.Equal(t, "hi", engine.capturedScanText())
		})
	}
}

func TestCodexBootstrapBlockPrecedesAPIKeyAndOAuthSideEffects(t *testing.T) {
	gin.SetMode(gin.TestMode)
	payload := []byte(`{
		"model":"gpt-5",
		"input":[{
			"type":"function_call_output",
			"namespace":"codex_app",
			"name":"automation_update",
			"output":"Automation: Scheduled review\nAutomation ID: wiki\nAutomation memory: $CODEX_HOME/automations/wiki/memory.md\nLast run: never\n\nblocked bootstrap prompt"
		}]
	}`)

	for _, accountType := range []string{service.AccountTypeAPIKey, service.AccountTypeOAuth} {
		t.Run(accountType, func(t *testing.T) {
			engine := &turnCountingEngine{
				mode:            securityaudit.ModeBlocking,
				captureSnapshot: true,
				decisions: []*securityaudit.PromptDecision{{
					Kind: securityaudit.DecisionBlock,
				}},
			}
			coordinator := securityaudit.NewCoordinator(nil, engine)
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
			groupID := int64(3)
			apiKey := &service.APIKey{
				ID: 9, UserID: 7, GroupID: &groupID,
				Group: &service.Group{ID: groupID, Platform: service.PlatformOpenAI},
			}
			account := &service.Account{ID: 11, Platform: service.PlatformOpenAI, Type: accountType}

			decision := runSecurityAudit(
				c, nil, coordinator, nil, apiKey,
				middleware2.AuthSubject{UserID: 7, Concurrency: 2},
				service.ContentModerationProtocolOpenAIResponses, "gpt-5", payload, "http",
			)

			require.NotNil(t, decision)
			require.Equal(t, securityaudit.DecisionBlock, decision.Kind)
			require.False(t, decision.AllowNextStage)
			require.Contains(t, engine.capturedScanText(), "blocked bootstrap prompt")
			require.Empty(t, recorder.Result().Header.Get("Content-Type"))

			accountSelections, billingChecks, concurrencyAcquisitions, upstreamDispatches := 0, 0, 0, 0
			if decision.AllowNextStage {
				accountSelections++
				_ = account.Type
				billingChecks++
				concurrencyAcquisitions++
				upstreamDispatches++
			}
			require.Zero(t, accountSelections)
			require.Zero(t, billingChecks)
			require.Zero(t, concurrencyAcquisitions)
			require.Zero(t, upstreamDispatches)
		})
	}
}

type blockingCompatibilityConfigStore struct {
	cfg securityaudit.ActiveConfig
}

func (s blockingCompatibilityConfigStore) Start(context.Context) error    { return nil }
func (s blockingCompatibilityConfigStore) Shutdown(context.Context) error { return nil }
func (s blockingCompatibilityConfigStore) Active() (securityaudit.ActiveConfig, bool) {
	return s.cfg, true
}
func (s blockingCompatibilityConfigStore) EffectiveMode() securityaudit.Mode {
	return securityaudit.ModeBlocking
}
func (s blockingCompatibilityConfigStore) BlockingActivationDegraded() bool { return false }
func (s blockingCompatibilityConfigStore) Public() (securityaudit.PublicConfig, error) {
	return securityaudit.PublicConfig{}, nil
}
func (s blockingCompatibilityConfigStore) Save(context.Context, securityaudit.UpdateConfigRequest, int64) (securityaudit.PublicConfig, error) {
	return securityaudit.PublicConfig{}, nil
}
func (s blockingCompatibilityConfigStore) RuntimeState() (int64, int64, *time.Time, string) {
	return 0, 0, nil, ""
}
func (s blockingCompatibilityConfigStore) Encrypt(value string) (string, error) { return value, nil }
func (s blockingCompatibilityConfigStore) Decrypt(value string) (string, error) { return value, nil }

func TestExtractionFailuresAllowAPIKeyAndOAuthDownstreamStages(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, accountType := range []string{service.AccountTypeAPIKey, service.AccountTypeOAuth} {
		t.Run(accountType, func(t *testing.T) {
			account := &service.Account{
				ID: 11, Platform: service.PlatformOpenAI, Type: accountType,
				Credentials: map[string]any{
					"api_key":      "api-key-credential",
					"access_token": "oauth-access-token",
				},
			}
			metrics := securityaudit.NewAtomicMetrics()
			prompt := securityaudit.NewPromptService(blockingCompatibilityConfigStore{cfg: securityaudit.ActiveConfig{
				RiskControlEnabled: true, Enabled: true, BlockingEnabled: true, AllGroups: true,
				Scanners: []string{"pii"}, Endpoints: []securityaudit.ActiveEndpoint{{ID: "guard", Enabled: true, TimeoutMS: 1000, InputLimit: 4096}},
			}}, nil, nil, nil, metrics)
			coordinator := securityaudit.NewCoordinator(nil, prompt)
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
			groupID := int64(3)
			apiKey := &service.APIKey{ID: 9, UserID: 7, GroupID: &groupID, Group: &service.Group{ID: groupID, Platform: service.PlatformOpenAI}}
			decision := runSecurityAudit(
				c, nil, coordinator, nil, apiKey, middleware2.AuthSubject{UserID: 7, Concurrency: 2},
				service.ContentModerationProtocolOpenAIResponses, "gpt-test",
				[]byte(`{"type":"future.client.event","payload":"unrecognized content"}`), "http",
			)
			require.NotNil(t, decision)
			require.Equal(t, securityaudit.DecisionAllow, decision.Kind)
			require.True(t, decision.AllowNextStage)

			accountSelections, billingChecks, concurrencyAcquisitions, upstreamDispatches := 0, 0, 0, 0
			selectedCredential := ""
			if decision.AllowNextStage {
				accountSelections++
				if account.IsOpenAIOAuth() {
					selectedCredential = account.GetOpenAIAccessToken()
				} else {
					selectedCredential = account.GetOpenAIProtocolAPIKey()
				}
				billingChecks++
				concurrencyAcquisitions++
				upstreamDispatches++
			}
			require.Equal(t, 1, accountSelections, accountType)
			require.Equal(t, 1, billingChecks, accountType)
			require.Equal(t, 1, concurrencyAcquisitions, accountType)
			require.Equal(t, 1, upstreamDispatches, accountType)
			if accountType == service.AccountTypeOAuth {
				require.Equal(t, "oauth-access-token", selectedCredential)
			} else {
				require.Equal(t, "api-key-credential", selectedCredential)
			}
			require.Equal(t, securityaudit.AuditMetricsSnapshot{ExtractionAttempted: 1, ExtractionFailed: 1}, metrics.AuditSnapshot())
		})
	}
}

type turnCountingEngine struct {
	mode            securityaudit.Mode
	enqueues        atomic.Int64
	evaluates       atomic.Int64
	decisions       []*securityaudit.PromptDecision
	captureSnapshot bool
	lastScanText    atomic.Value
}

func (e *turnCountingEngine) EffectiveMode() securityaudit.Mode { return e.mode }
func (e *turnCountingEngine) Enqueue(_ context.Context, req securityaudit.Request) error {
	e.enqueues.Add(1)
	if e.captureSnapshot {
		if snapshot, err := securityaudit.ExtractPromptSnapshot(req); err == nil {
			e.lastScanText.Store(snapshot.ScanText)
		}
	}
	return nil
}
func (e *turnCountingEngine) Evaluate(_ context.Context, req securityaudit.Request) (*securityaudit.PromptDecision, error) {
	call := e.evaluates.Add(1)
	if e.captureSnapshot {
		if snapshot, err := securityaudit.ExtractBlockingPromptSnapshot(req, true); err == nil {
			e.lastScanText.Store(snapshot.ScanText)
		}
	}
	if int(call) <= len(e.decisions) {
		return e.decisions[call-1], nil
	}
	return &securityaudit.PromptDecision{Kind: securityaudit.DecisionAllow, AllowNextStage: true}, nil
}

func (e *turnCountingEngine) capturedScanText() string {
	if value := e.lastScanText.Load(); value != nil {
		if scanText, ok := value.(string); ok {
			return scanText
		}
	}
	return ""
}
