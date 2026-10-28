//go:build unit || !integration

package handler

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/config"
	"github.com/LuckyKuang/sub2api-plus/internal/securityaudit"
	middleware "github.com/LuckyKuang/sub2api-plus/internal/server/middleware"
	"github.com/LuckyKuang/sub2api-plus/internal/service"
	"github.com/LuckyKuang/sub2api-plus/internal/testutil"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func newGatewayAdmissionForTest() *OpenAIGatewayHandler {
	return &OpenAIGatewayHandler{unfinishedTasks: service.NewGatewayUnfinishedTaskService(&testutil.StubGatewayUnfinishedTaskRepo{}, nil), concurrencyHelper: NewConcurrencyHelper(service.NewConcurrencyService(&metadataConcurrencyCache{}), SSEPingFormatNone, 0)}
}

type admissionUnavailableCache struct {
	helperConcurrencyCacheStub
	audited       func() bool
	t             *testing.T
	full          bool
	attemptOwner  string
	cleanupOwner  string
	cleanupUserID int64
}

func (s *admissionUnavailableCache) AcquireUserSlot(_ context.Context, _ int64, _ int, owner string) (bool, error) {
	s.userAcquireCalls++
	s.attemptOwner = owner
	require.True(s.t, s.audited(), "audit must run before admission")
	if s.full {
		return false, nil
	}
	return false, errors.New("redis unreachable")
}

func (s *admissionUnavailableCache) ReleaseUserSlot(ctx context.Context, userID int64, owner string) error {
	s.cleanupUserID = userID
	s.cleanupOwner = owner
	return s.helperConcurrencyCacheStub.ReleaseUserSlot(ctx, userID, owner)
}

// Independent search/audio/count/file entrypoints must fail before selecting
// an account, reserving money, or contacting a provider when Redis cannot
// confirm admission. Nil downstream services make any forbidden effect fail.
func TestUserRequestAdmissionIndependentEntrypoints(t *testing.T) {
	for _, full := range []bool{false, true} {
		name := "cache-unavailable"
		if full {
			name = "user-full"
		}
		t.Run(name, func(t *testing.T) { testUserRequestAdmissionIndependentEntrypoints(t, full) })
	}
}

func testUserRequestAdmissionIndependentEntrypoints(t *testing.T, full bool) {
	t.Helper()
	for _, endpoint := range []string{"realtime", "tts", "web_search", "x_search", "grok_count", "openai_count", "input_tokens", "anthropic_count"} {
		t.Run(endpoint, func(t *testing.T) {
			engine := &pricingAdmissionEngine{kind: securityaudit.DecisionAllow}
			cache := &admissionUnavailableCache{t: t, full: full, audited: func() bool { return engine.calls.Load() == 1 }}
			helper := NewConcurrencyHelper(service.NewConcurrencyService(cache), SSEPingFormatNone, 0)
			cfg := &config.Config{}
			billing := service.NewBillingCacheService(nil, nil, nil, nil, nil, nil, cfg, nil)
			t.Cleanup(billing.Stop)
			openAI := &OpenAIGatewayHandler{gatewayService: &service.OpenAIGatewayService{}, apiKeyService: &service.APIKeyService{}, concurrencyHelper: helper, securityAuditCoordinator: securityaudit.NewCoordinator(nil, engine), cfg: cfg, billingCacheService: billing}
			gateway := &GatewayHandler{concurrencyHelper: helper, securityAuditCoordinator: securityaudit.NewCoordinator(nil, engine), cfg: cfg, billingCacheService: billing}
			body := `{"model":"grok-4.5","input":"hello","messages":[{"role":"user","content":"hello"}],"query":"hello"}`
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/test", strings.NewReader(body))
			c.Request.Header.Set("Content-Type", "application/json")
			id := int64(3)
			key := &service.APIKey{ID: 9, UserID: 7, User: &service.User{ID: 7, Concurrency: 2}, GroupID: &id, Group: &service.Group{ID: id, Platform: service.PlatformGrok, RateMultiplier: 1}}
			if endpoint == "input_tokens" || endpoint == "openai_count" {
				key.Group.Platform = service.PlatformOpenAI
				key.Group.AllowMessagesDispatch = true
			}
			if endpoint == "anthropic_count" {
				key.Group.Platform = service.PlatformAnthropic
			}
			c.Set(string(middleware.ContextKeyAPIKey), key)
			c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: 7, Concurrency: 2})
			started := time.Now()
			switch endpoint {
			case "realtime":
				c.Request.Method = http.MethodGet
				c.Request.Header.Set("Connection", "Upgrade")
				c.Request.Header.Set("Upgrade", "websocket")
				openAI.GrokRealtime(c)
			case "tts":
				openAI.GrokVoice(c, "tts")
			case "web_search":
				gateway.WebSearch(c)
			case "x_search":
				gateway.XSearch(c)
			case "grok_count":
				openAI.GrokCountTokens(c)
			case "openai_count":
				openAI.CountTokens(c)
			case "input_tokens":
				openAI.ResponsesInputTokens(c)
			case "anthropic_count":
				gateway.CountTokens(c)
			}
			require.Less(t, time.Since(started), time.Second)
			expectedStatus := http.StatusServiceUnavailable
			if full {
				expectedStatus = http.StatusTooManyRequests
			}
			require.Equal(t, expectedStatus, w.Code, w.Body.String())
			require.Equal(t, 1, cache.userAcquireCalls)
			require.Zero(t, cache.waitIncrementCalls)
			require.Zero(t, cache.waitDecrementCalls)
			require.Zero(t, cache.apiKeyTrackCalls)
			if full {
				require.Zero(t, cache.userReleaseCalls, "a confirmed full pool has no uncertain owner to reclaim")
			} else {
				// A failed Redis reply cannot prove whether its Lua write
				// committed. Reclaim exactly this attempted owner, even though
				// the external execution was never admitted.
				require.Equal(t, 1, cache.userReleaseCalls)
				require.NotEmpty(t, cache.attemptOwner)
				require.Equal(t, cache.attemptOwner, cache.cleanupOwner)
				require.Equal(t, int64(7), cache.cleanupUserID)
			}
			require.Zero(t, cache.accountAcquireCalls)
			_, selected := c.Get(opsAccountIDKey)
			require.False(t, selected)
		})
	}
}

func TestUserRequestAdmissionControlReleaseAndNoPricing(t *testing.T) {
	for _, kind := range []securityaudit.DecisionKind{securityaudit.DecisionAllow, securityaudit.DecisionBlock} {
		t.Run(string(kind), func(t *testing.T) {
			engine := &pricingAdmissionEngine{kind: kind}
			cache := &helperConcurrencyCacheStub{userSeq: []bool{true}}
			h := &OpenAIGatewayHandler{securityAuditCoordinator: securityaudit.NewCoordinator(nil, engine), concurrencyHelper: NewConcurrencyHelper(service.NewConcurrencyService(cache), SSEPingFormatNone, 0), gatewayService: &service.OpenAIGatewayService{}}
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequest(http.MethodGet, "/v1/images/tasks", nil)
			c.Set(string(middleware.ContextKeyAPIKey), &service.APIKey{ID: 9, UserID: 7})
			c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: 7, Concurrency: 1})
			release, ok := h.admitControl(c)
			if kind == securityaudit.DecisionBlock {
				require.False(t, ok)
				require.Zero(t, cache.userAcquireCalls)
				return
			}
			require.True(t, ok)
			require.Equal(t, 1, cache.userAcquireCalls)
			require.Nil(t, service.BillingPricingSnapshotFromContext(c.Request.Context()))
			release()
			release()
			require.Equal(t, 1, cache.userReleaseCalls)
		})
	}
}

func TestUserRequestAdmissionFullControlRejectsImmediatelyAfterAudit(t *testing.T) {
	// Content-free task/file/catalog calls are external requests too. They must
	// enter audit and then fail at the cap without a user queue or downstream work.
	for _, protocol := range []string{service.ContentModerationProtocolOpenAIResponses, service.ContentModerationProtocolAnthropicMessages, service.ContentModerationProtocolGemini} {
		t.Run(protocol, func(t *testing.T) {
			engine := &pricingAdmissionEngine{kind: securityaudit.DecisionAllow}
			cache := &admissionUnavailableCache{t: t, full: true, audited: func() bool { return engine.calls.Load() == 1 }}
			helper := NewConcurrencyHelper(service.NewConcurrencyService(cache), SSEPingFormatNone, 0)
			coordinator := securityaudit.NewCoordinator(nil, engine)
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequest(http.MethodGet, "/v1/images/tasks", nil)
			c.Set(string(middleware.ContextKeyAPIKey), &service.APIKey{ID: 9, UserID: 7})
			c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: 7, Concurrency: 3})
			var release func()
			var admitted bool
			started := time.Now()
			if protocol == service.ContentModerationProtocolOpenAIResponses {
				h := &OpenAIGatewayHandler{securityAuditCoordinator: coordinator, concurrencyHelper: helper}
				release, admitted = h.admitControl(c)
			} else {
				h := &GatewayHandler{securityAuditCoordinator: coordinator, concurrencyHelper: helper}
				release, admitted = h.admitControl(c, protocol)
			}
			require.Less(t, time.Since(started), time.Second)
			require.False(t, admitted)
			require.Nil(t, release)
			require.Equal(t, http.StatusTooManyRequests, w.Code, w.Body.String())
			require.Equal(t, 1, cache.userAcquireCalls)
			require.Zero(t, cache.waitIncrementCalls)
			require.Zero(t, cache.waitDecrementCalls)
			require.Zero(t, cache.accountAcquireCalls)
			require.Zero(t, cache.apiKeyTrackCalls)
			require.Zero(t, cache.userReleaseCalls)
			require.Nil(t, service.BillingPricingSnapshotFromContext(c.Request.Context()))
		})
	}
}
