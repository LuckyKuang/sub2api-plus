//go:build unit || !integration

package handler

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/config"
	"github.com/LuckyKuang/sub2api-plus/internal/securityaudit"
	"github.com/LuckyKuang/sub2api-plus/internal/server/middleware"
	"github.com/LuckyKuang/sub2api-plus/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type gatewayRequestRPMCache struct {
	mu     sync.Mutex
	users  map[int64]int
	groups map[[2]int64]int
	err    error
}

func (s *gatewayRequestRPMCache) IncrementUserRPM(_ context.Context, id int64) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.users == nil {
		s.users = make(map[int64]int)
	}
	s.users[id]++
	return s.users[id], s.err
}

func (s *gatewayRequestRPMCache) IncrementUserGroupRPM(_ context.Context, user, group int64) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.groups == nil {
		s.groups = make(map[[2]int64]int)
	}
	key := [2]int64{user, group}
	s.groups[key]++
	return s.groups[key], s.err
}

func (s *gatewayRequestRPMCache) GetUserRPM(_ context.Context, id int64) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.users[id], nil
}

func (s *gatewayRequestRPMCache) GetUserGroupRPM(_ context.Context, user, group int64) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.groups[[2]int64{user, group}], nil
}

func gatewayRequestRPMHandlers(t *testing.T, slots *helperConcurrencyCacheStub, rpm *gatewayRequestRPMCache, cfg *config.Config) (*GatewayHandler, *OpenAIGatewayHandler, *service.BillingCacheService) {
	t.Helper()
	billing := service.NewBillingCacheService(nil, nil, nil, nil, rpm, nil, cfg, nil)
	t.Cleanup(billing.Stop)
	concurrency := service.NewConcurrencyService(slots)
	// Exercise production constructor wiring; a manually injected helper would
	// not detect an entrypoint silently losing its RPM dependency.
	gateway := NewGatewayHandler(nil, nil, nil, nil, nil, concurrency, billing, nil, nil, nil, nil, nil, nil, cfg, nil)
	openAI := NewOpenAIGatewayHandler(nil, concurrency, billing, nil, nil, nil, nil, nil, cfg)
	return gateway, openAI, billing
}

func gatewayRequestRPMContext(t *testing.T, keyID, userID int64, limit int) (*gin.Context, *httptest.ResponseRecorder, *service.APIKey) {
	t.Helper()
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	groupID := int64(9)
	key := &service.APIKey{ID: keyID, UserID: userID, User: &service.User{ID: userID, Concurrency: 3, RPMLimit: limit}, GroupID: &groupID, Group: &service.Group{ID: groupID, Platform: service.PlatformGrok, RPMLimit: 10}}
	c.Set(string(middleware.ContextKeyAPIKey), key)
	c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: userID, Concurrency: 3})
	return c, w, key
}

// Requirement: authenticated external gateway calls share the user and
// (user,group) RPM pools across keys, after audit and user-capacity admission.
// Rejection must be 429 with retry information and must release its own slot.
func TestGatewayRequestRPMControlAdmissionAcrossKeys(t *testing.T) {
	for _, protocol := range []string{service.ContentModerationProtocolOpenAIResponses, service.ContentModerationProtocolAnthropicMessages, service.ContentModerationProtocolGemini} {
		t.Run(protocol, func(t *testing.T) {
			slots := &helperConcurrencyCacheStub{userSeq: []bool{true, true, true}}
			rpm := &gatewayRequestRPMCache{}
			gateway, openAI, _ := gatewayRequestRPMHandlers(t, slots, rpm, &config.Config{})
			engine := &pricingAdmissionEngine{kind: securityaudit.DecisionAllow}
			gateway.securityAuditCoordinator = securityaudit.NewCoordinator(nil, engine)
			openAI.securityAuditCoordinator = gateway.securityAuditCoordinator
			admit := func(c *gin.Context) (func(), bool) {
				if protocol == service.ContentModerationProtocolOpenAIResponses {
					return openAI.admitControl(c)
				}
				return gateway.admitControl(c, protocol)
			}
			c, _, _ := gatewayRequestRPMContext(t, 11, 7, 1)
			release, ok := admit(c)
			require.True(t, ok)
			release()
			release()
			c, w, _ := gatewayRequestRPMContext(t, 12, 7, 1)
			release, ok = admit(c)
			if release != nil {
				defer release()
			}
			require.False(t, ok, "another API key must not bypass the user's RPM")
			require.Nil(t, release)
			require.Equal(t, http.StatusTooManyRequests, w.Code, w.Body.String())
			errType, code := service.OpsLocalErrorType(c)
			require.Equal(t, "rate_limit_error", errType)
			require.Equal(t, "rate_limit_exceeded", code, "local RPM rejection must not be recorded as an upstream failure")
			retry, err := strconv.Atoi(w.Header().Get("Retry-After"))
			require.NoError(t, err)
			require.GreaterOrEqual(t, retry, 1)
			require.LessOrEqual(t, retry, 60)
			require.Equal(t, 2, slots.userReleaseCalls)
			require.Equal(t, 1, slots.apiKeyReleaseCalls, "RPM-rejected work must not acquire API-key execution statistics")
			require.Zero(t, slots.accountAcquireCalls)
			require.Zero(t, slots.waitIncrementCalls)
			require.Equal(t, int64(2), engine.calls.Load())
			c, _, _ = gatewayRequestRPMContext(t, 13, 8, 1)
			release, ok = admit(c)
			require.True(t, ok, "a different user owns an independent RPM pool")
			release()
			require.Equal(t, map[int64]int{7: 2, 8: 1}, rpm.users)
			require.Equal(t, map[[2]int64]int{{7, 9}: 2, {8, 9}: 1}, rpm.groups)
		})
	}
}

func TestGatewayRequestRPMFullUserDoesNotSpendRPM(t *testing.T) {
	slots := &helperConcurrencyCacheStub{userSeq: []bool{false}}
	rpm := &gatewayRequestRPMCache{}
	_, h, _ := gatewayRequestRPMHandlers(t, slots, rpm, &config.Config{})
	c, w, _ := gatewayRequestRPMContext(t, 11, 7, 1)
	release, ok := h.admitControl(c)
	require.False(t, ok)
	require.Nil(t, release)
	require.Equal(t, http.StatusTooManyRequests, w.Code)
	require.Empty(t, rpm.users)
	require.Empty(t, rpm.groups)
	require.Zero(t, slots.waitIncrementCalls)
	require.Zero(t, slots.apiKeyTrackCalls)
}

func TestGatewayRequestRPMInternalRechecksAndAsyncHandoffCountOnce(t *testing.T) {
	slots := &helperConcurrencyCacheStub{userSeq: []bool{true, true, true}}
	rpm := &gatewayRequestRPMCache{}
	_, h, billing := gatewayRequestRPMHandlers(t, slots, rpm, &config.Config{})
	c, _, key := gatewayRequestRPMContext(t, 11, 7, 2)
	release, ok := h.admitControl(c)
	require.True(t, ok)
	release()
	require.NoError(t, billing.CheckRequestRPM(c.Request.Context(), key.User, key.Group))
	worker, _, cancel := newAsyncImageContext(c, []byte(`{"model":"grok-2-image"}`), time.Minute)
	defer cancel()
	workerRelease, err := h.concurrencyHelper.AcquireUserSlot(worker, 7, 3)
	require.NoError(t, err)
	workerRelease()
	require.NoError(t, billing.CheckRequestRPM(worker.Request.Context(), key.User, key.Group))
	require.Equal(t, 1, rpm.users[7], "async execution and billing rechecks belong to one external request")
	require.Equal(t, 1, rpm.groups[[2]int64{7, 9}])
	c, _, _ = gatewayRequestRPMContext(t, 12, 7, 2)
	release, ok = h.admitControl(c)
	require.True(t, ok)
	release()
	require.Equal(t, 2, rpm.users[7], "a new HTTP request must get a fresh admission scope")
}

func TestGatewayRequestRPMIndependentEntrypointsRejectBeforeSideEffects(t *testing.T) {
	for _, endpoint := range []string{"grok_count", "batch_submit", "models"} {
		t.Run(endpoint, func(t *testing.T) {
			slots := &helperConcurrencyCacheStub{userSeq: []bool{true}}
			rpm := &gatewayRequestRPMCache{users: map[int64]int{7: 1}}
			gateway, openAI, _ := gatewayRequestRPMHandlers(t, slots, rpm, &config.Config{})
			c, w, _ := gatewayRequestRPMContext(t, 12, 7, 1)
			// Nil downstream services are intentional: any account, job or
			// provider side effect after failed admission makes this test fail.
			switch endpoint {
			case "grok_count":
				c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages/count_tokens", strings.NewReader(`{"model":"grok-4.5","messages":[{"role":"user","content":"hello"}]}`))
				openAI.GrokCountTokens(c)
			case "batch_submit":
				c.Request = httptest.NewRequest(http.MethodPost, "/v1/images/batches", strings.NewReader(`{"model":"grok-2-image","items":[{"prompt":"hello"}]}`))
				(&BatchImageHandler{openAI: openAI}).Submit(c)
			case "models":
				gateway.Models(c)
			}
			require.Equal(t, http.StatusTooManyRequests, w.Code, w.Body.String())
			require.NotEmpty(t, w.Header().Get("Retry-After"))
			require.Equal(t, 1, slots.userReleaseCalls)
			require.Zero(t, slots.apiKeyTrackCalls)
			require.Zero(t, slots.apiKeyReleaseCalls)
			require.Zero(t, slots.accountAcquireCalls)
		})
	}
}

func TestGatewayRequestRPMPreservesSimpleAndCacheFailurePolicy(t *testing.T) {
	for _, simple := range []bool{false, true} {
		t.Run(strconv.FormatBool(simple), func(t *testing.T) {
			slots := &helperConcurrencyCacheStub{userSeq: []bool{true}}
			rpm := &gatewayRequestRPMCache{err: errors.New("redis unavailable")}
			cfg := &config.Config{}
			if simple {
				cfg.RunMode = config.RunModeSimple
			}
			_, h, _ := gatewayRequestRPMHandlers(t, slots, rpm, cfg)
			c, _, _ := gatewayRequestRPMContext(t, 11, 7, 1)
			release, ok := h.admitControl(c)
			require.True(t, ok)
			release()
			if simple {
				require.Empty(t, rpm.users)
				require.Empty(t, rpm.groups)
			} else {
				require.Equal(t, 1, rpm.users[7])
				require.Equal(t, 1, rpm.groups[[2]int64{7, 9}])
			}
		})
	}
}

func TestGatewayRequestRPMErrorEnvelopes(t *testing.T) {
	for _, response := range []struct {
		name  string
		write func(*gin.Context, error)
	}{
		{"compatible", func(c *gin.Context, err error) { (&GatewayHandler{}).handleConcurrencyError(c, err, "user", false) }},
		{"openai", func(c *gin.Context, err error) {
			(&OpenAIGatewayHandler{}).handleConcurrencyError(c, err, "user", false)
		}},
		{"gemini", func(c *gin.Context, err error) { googleConcurrencyError(c, err, "user") }},
	} {
		t.Run(response.name, func(t *testing.T) {
			c, w, _ := gatewayRequestRPMContext(t, 11, 7, 1)
			response.write(c, service.ErrUserRPMExceeded)
			require.Equal(t, http.StatusTooManyRequests, w.Code, w.Body.String())
			if response.name == "gemini" {
				require.Contains(t, w.Body.String(), `"status":"RESOURCE_EXHAUSTED"`)
				errType, code := service.OpsLocalErrorType(c)
				require.Equal(t, "rate_limit_error", errType)
				require.Equal(t, "rate_limit_exceeded", code)
			} else {
				require.Contains(t, w.Body.String(), "rate_limit_exceeded")
			}
			require.NotEmpty(t, w.Header().Get("Retry-After"))
		})
	}
}
