//go:build unit

package handler

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/LuckyKuang/sub2api-plus/internal/config"
	"github.com/LuckyKuang/sub2api-plus/internal/securityaudit"
	middleware "github.com/LuckyKuang/sub2api-plus/internal/server/middleware"
	"github.com/LuckyKuang/sub2api-plus/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestBillingPricingFailureFollowsAuditAndPrecedesSlotsAndSelection(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tc := range []struct{ endpoint, path, platform, body string }{
		{"gateway-messages", "/v1/messages", service.PlatformAnthropic, `{"model":"claude-sonnet-4-6","max_tokens":64,"messages":[{"role":"user","content":"allowed content"}]}`},
		{"gateway-chat", "/v1/chat/completions", service.PlatformAnthropic, `{"model":"claude-sonnet-4-6","messages":[{"role":"user","content":"allowed content"}]}`},
		{"gateway-responses", "/v1/responses", service.PlatformAnthropic, `{"model":"claude-sonnet-4-6","input":"allowed content"}`},
		{"openai-responses", "/v1/responses", service.PlatformOpenAI, `{"model":"gpt-5.1","input":"allowed content"}`},
		{"openai-images", "/v1/images/generations", service.PlatformOpenAI, `{"model":"gpt-image-2","prompt":"allowed content"}`},
		{"gemini-generate", "/v1beta/models/gemini-2.5-flash:generateContent", service.PlatformGemini, `{"contents":[{"role":"user","parts":[{"text":"allowed content"}]}]}`},
	} {
		t.Run(tc.endpoint, func(t *testing.T) {
			engine := &pricingAdmissionEngine{kind: securityaudit.DecisionAllow}
			cache := &domesticAuditConcurrencyCache{}
			cfg := &config.Config{}
			billing := service.NewBillingCacheService(nil, nil, nil, nil, nil, nil, cfg, nil)
			t.Cleanup(billing.Stop)
			concurrency := NewConcurrencyHelper(service.NewConcurrencyService(cache), SSEPingFormatNone, 0)
			coordinator := securityaudit.NewCoordinator(nil, engine)
			gateway := &GatewayHandler{gatewayService: &service.GatewayService{}, cfg: cfg, billingCacheService: billing, concurrencyHelper: concurrency, apiKeyService: &service.APIKeyService{}, securityAuditCoordinator: coordinator}
			openai := &OpenAIGatewayHandler{gatewayService: &service.OpenAIGatewayService{}, cfg: cfg, billingCacheService: billing, concurrencyHelper: concurrency, apiKeyService: &service.APIKeyService{}, securityAuditCoordinator: coordinator}
			var send func(*gin.Context)
			switch tc.endpoint {
			case "gateway-messages":
				send = gateway.Messages
			case "gateway-chat":
				send = gateway.ChatCompletions
			case "gateway-responses":
				send = gateway.Responses
			case "openai-responses":
				send = openai.Responses
			case "openai-images":
				send = openai.Images
			case "gemini-generate":
				send = gateway.GeminiV1BetaModels
			}
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Request = httptest.NewRequest(http.MethodPost, tc.path, strings.NewReader(tc.body))
			c.Request.Header.Set("Content-Type", "application/json")
			if tc.endpoint == "gemini-generate" {
				c.Params = gin.Params{{Key: "modelAction", Value: "gemini-2.5-flash:generateContent"}}
			}
			groupID := int64(3)
			key := &service.APIKey{ID: 9, UserID: 7, User: &service.User{ID: 7, Concurrency: 1, Balance: 100}, GroupID: &groupID,
				Group: &service.Group{ID: groupID, Platform: tc.platform, RateMultiplier: 1e9, Hydrated: true, AllowImageGeneration: true}}
			c.Set(string(middleware.ContextKeyAPIKey), key)
			c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: 7, Concurrency: 1})
			send(c)
			require.Equal(t, http.StatusServiceUnavailable, recorder.Code, recorder.Body.String())
			require.Contains(t, recorder.Body.String(), "BILLING_RATE_UNAVAILABLE")
			require.Equal(t, int64(1), engine.calls.Load(), "valid ingress still completes its content audit")
			require.False(t, engine.observedSnapshot.Load(), "capture remains after the audit")
			require.Zero(t, cache.userAcquisitions, "an unavailable price must not reserve capacity")
			_, selected := c.Get(opsAccountIDKey)
			require.False(t, selected)
			require.Empty(t, opsSecurityAuditDenialCode(c), "billing dependency failure cannot be attributed to content policy")
			if tc.endpoint == "gemini-generate" {
				require.Contains(t, recorder.Body.String(), "gateway.billing")
			}
		})
	}
}

func TestBillingPricingWSFirstTurnRejectsIndependentlyFromSuccessfulAudit(t *testing.T) {
	engine := &pricingAdmissionEngine{kind: securityaudit.DecisionAllow}
	h := &OpenAIGatewayHandler{gatewayService: &service.OpenAIGatewayService{}, securityAuditCoordinator: securityaudit.NewCoordinator(nil, engine)}
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodGet, "/v1/responses", nil)
	id := int64(3)
	key := &service.APIKey{GroupID: &id, Group: &service.Group{ID: id, Platform: service.PlatformOpenAI, RateMultiplier: 1e9}}
	decision := h.checkSecurityAuditStage(c, nil, key, middleware.AuthSubject{UserID: 7}, service.ContentModerationProtocolOpenAIResponses, "gpt-5.1", []byte(`{"type":"response.create","response":{"input":"allowed content"}}`), "first_turn")
	require.NotNil(t, decision)
	require.Equal(t, billingAdmissionUnavailableKind, decision.Kind)
	require.Equal(t, "BILLING_RATE_UNAVAILABLE", decision.ErrorCode)
	require.Equal(t, http.StatusServiceUnavailable, decision.HTTPStatus)
	require.Equal(t, int64(1), engine.calls.Load())
	require.False(t, decision.AllowNextStage)
	require.Empty(t, opsSecurityAuditDenialCode(c))
	// The audit result itself stays allow; extraction compatibility is untouched.
	audit := runSecurityAudit(c, nil, h.securityAuditCoordinator, nil, key, middleware.AuthSubject{UserID: 7}, service.ContentModerationProtocolOpenAIResponses, "gpt-5.1", []byte(`{"input":"allowed content"}`), "http")
	require.True(t, audit.AllowNextStage)
}
