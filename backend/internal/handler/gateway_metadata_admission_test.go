//go:build unit

package handler

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/LuckyKuang/sub2api-plus/internal/securityaudit"
	middleware "github.com/LuckyKuang/sub2api-plus/internal/server/middleware"
	"github.com/LuckyKuang/sub2api-plus/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type metadataAdmissionFailureCache struct {
	helperConcurrencyCacheStub
	t       *testing.T
	audited func() bool
}

func (s *metadataAdmissionFailureCache) AcquireUserSlot(context.Context, int64, int, string) (bool, error) {
	s.userAcquireCalls++
	require.True(s.t, s.audited(), "metadata audit precedes global user admission")
	return false, errors.New("redis unavailable")
}

func TestGatewayMetadataEndpointsAuditAndDenyUnconfirmedGlobalAdmission(t *testing.T) {
	for _, endpoint := range []string{"models", "codex", "openai_codex", "antigravity", "usage", "local_quota", "gemini_list", "gemini_get"} {
		for _, scenario := range []string{"redis_failure", "nil_helper", "audit_block"} {
			t.Run(endpoint+"/"+scenario, func(t *testing.T) {
				kind := securityaudit.DecisionAllow
				if scenario == "audit_block" {
					kind = securityaudit.DecisionBlock
				}
				engine := &pricingAdmissionEngine{kind: kind}
				cache := &metadataAdmissionFailureCache{t: t, audited: func() bool { return engine.calls.Load() == 1 }}
				helper := NewConcurrencyHelper(service.NewConcurrencyService(cache), SSEPingFormatNone, 0)
				if scenario == "nil_helper" {
					helper = nil
				}
				coordinator := securityaudit.NewCoordinator(nil, engine)
				gateway := &GatewayHandler{concurrencyHelper: helper, securityAuditCoordinator: coordinator}
				openAI := &OpenAIGatewayHandler{concurrencyHelper: helper, securityAuditCoordinator: coordinator}
				recorder := httptest.NewRecorder()
				c, _ := gin.CreateTestContext(recorder)
				c.Request = httptest.NewRequest(http.MethodGet, "/v1/models", nil)
				groupID := int64(3)
				key := &service.APIKey{ID: 9, UserID: 7, GroupID: &groupID, User: &service.User{ID: 7}, Group: &service.Group{ID: groupID, Platform: service.PlatformOpenAI}}
				c.Set(string(middleware.ContextKeyAPIKey), key)
				c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: 7, Concurrency: 3})
				switch endpoint {
				case "models":
					gateway.Models(c)
				case "codex":
					gateway.CodexModels(c)
				case "openai_codex":
					openAI.CodexModels(c)
				case "antigravity":
					gateway.AntigravityModels(c)
				case "usage":
					gateway.Usage(c)
				case "local_quota":
					openAI.CodexLocalGroupQuotaUsage(c)
				case "gemini_list":
					key.Group.Platform = service.PlatformGemini
					gateway.GeminiV1BetaListModels(c)
				case "gemini_get":
					key.Group.Platform = service.PlatformGemini
					c.Params = gin.Params{{Key: "model", Value: "gemini-3.8-flash"}}
					gateway.GeminiV1BetaGetModel(c)
				}
				expectedStatus := http.StatusServiceUnavailable
				if scenario == "audit_block" {
					expectedStatus = http.StatusForbidden
				}
				require.Equal(t, expectedStatus, recorder.Code, recorder.Body.String())
				require.EqualValues(t, 1, engine.calls.Load())
				if scenario == "redis_failure" {
					require.Equal(t, 1, cache.userAcquireCalls)
				} else {
					require.Zero(t, cache.userAcquireCalls)
				}
				require.Zero(t, cache.accountAcquireCalls)
				require.Nil(t, service.BillingPricingSnapshotFromContext(c.Request.Context()), "metadata is independent of current billing dependencies")
				_, selected := c.Get(opsAccountIDKey)
				require.False(t, selected, "no metadata account lookup before confirmed user admission")
			})
		}
	}
}

func TestGatewayMetadataStaticResponseReleasesOneUserLease(t *testing.T) {
	engine := &pricingAdmissionEngine{kind: securityaudit.DecisionAllow}
	cache := &helperConcurrencyCacheStub{userSeq: []bool{true}}
	h := &GatewayHandler{securityAuditCoordinator: securityaudit.NewCoordinator(nil, engine), concurrencyHelper: NewConcurrencyHelper(service.NewConcurrencyService(cache), SSEPingFormatNone, 0)}
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodGet, "/antigravity/models", nil)
	c.Set(string(middleware.ContextKeyAPIKey), &service.APIKey{ID: 9, UserID: 7, Group: &service.Group{Platform: service.PlatformAntigravity}})
	c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: 7, Concurrency: 3})
	h.AntigravityModels(c)
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	require.Equal(t, 1, cache.userAcquireCalls)
	require.Equal(t, 1, cache.userReleaseCalls)
	require.Equal(t, 1, cache.apiKeyTrackCalls)
	require.Equal(t, 1, cache.apiKeyReleaseCalls)
	require.Nil(t, service.BillingPricingSnapshotFromContext(c.Request.Context()))
}
