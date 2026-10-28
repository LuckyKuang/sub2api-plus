//go:build unit || !integration

package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/LuckyKuang/sub2api-plus/internal/config"
	"github.com/LuckyKuang/sub2api-plus/internal/domain"
	"github.com/LuckyKuang/sub2api-plus/internal/securityaudit"
	middleware2 "github.com/LuckyKuang/sub2api-plus/internal/server/middleware"
	"github.com/LuckyKuang/sub2api-plus/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// Requirement: a Composite key can use already implemented native Grok APIs.
// Their API contract fixes the supplier, while the original group owns access,
// quota and the public meter's rate. Audit rejection must precede any billing.
func TestCompositeGrokNativeAuditUsesEndpointSupplierAndPublicMeter(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, endpoint := range []string{"tts", "stt", "custom-voices", "realtime", "web_search", "x_search"} {
		for _, platform := range []string{service.PlatformGrok, service.PlatformComposite} {
			t.Run(platform+"/"+endpoint, func(t *testing.T) {
				engine := blockingHandlerPromptEngine()
				coordinator := securityaudit.NewCoordinator(nil, engine)
				billing := service.NewBillingCacheService(nil, nil, nil, nil, nil, nil, &config.Config{}, nil)
				t.Cleanup(billing.Stop)
				openAI := &OpenAIGatewayHandler{gatewayService: &service.OpenAIGatewayService{}, apiKeyService: &service.APIKeyService{}, concurrencyHelper: newGatewayAdmissionForTest().concurrencyHelper, billingCacheService: billing, securityAuditCoordinator: coordinator}
				gateway := &GatewayHandler{securityAuditCoordinator: coordinator}
				recorder := httptest.NewRecorder()
				c, _ := gin.CreateTestContext(recorder)
				c.Request = httptest.NewRequest(http.MethodPost, "/v1/"+endpoint, strings.NewReader(`{"input":"blocked speech","query":"blocked search"}`))
				c.Request.Header.Set("Content-Type", "application/json")
				if endpoint == "realtime" {
					c.Request = httptest.NewRequest(http.MethodGet, "/v1/realtime", nil)
					c.Request.Header.Set("Connection", "Upgrade")
					c.Request.Header.Set("Upgrade", "websocket")
				}
				groupID := int64(3)
				key := &service.APIKey{ID: 9, UserID: 7, User: &service.User{ID: 7}, GroupID: &groupID, Group: &service.Group{ID: groupID, Platform: platform, RateMultiplier: 1}}
				c.Set(string(middleware2.ContextKeyAPIKey), key)
				c.Set(string(middleware2.ContextKeyUser), middleware2.AuthSubject{UserID: 7, Concurrency: 1})
				switch endpoint {
				case "web_search":
					gateway.WebSearch(c)
				case "x_search":
					gateway.XSearch(c)
				case "realtime":
					openAI.GrokRealtime(c)
				default:
					openAI.GrokVoice(c, endpoint)
				}
				require.Equal(t, http.StatusForbidden, recorder.Code)
				require.Contains(t, recorder.Body.String(), securityaudit.ErrorCodeBlocked)
				require.Nil(t, service.BillingPricingSnapshotFromContext(c.Request.Context()))
				evaluated, _, requests := engine.snapshot()
				require.Equal(t, 1, evaluated)
				require.Len(t, requests, 1)
				require.Equal(t, service.PlatformGrok, requests[0].Provider)
				expectedModel := endpoint
				switch endpoint {
				case "web_search":
					expectedModel = "grok-web-search"
				case "x_search":
					expectedModel = "grok-x-search"
				case "realtime":
					expectedModel = "grok-voice-latest"
				}
				require.Equal(t, expectedModel, requests[0].Model)
				require.Equal(t, platform, key.Group.Platform, "supplier binding must not replace customer membership")
			})
		}
	}
}

// Requirement: fixed native meters choose policy by their billed public SKU,
// never the unrelated text model used to select a capable account.
func TestCompositeGrokNativeAdmissionFreezesSupplierAndMeter(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, endpoint := range []string{"tts", "stt", "web_search", "x_search"} {
		t.Run(endpoint, func(t *testing.T) {
			engine := &pricingAdmissionEngine{kind: securityaudit.DecisionAllow}
			coordinator := securityaudit.NewCoordinator(nil, engine)
			cache := newHandlerInflightCache(0)
			cfg := &config.Config{}
			billing := service.NewBillingCacheService(cache, nil, nil, nil, nil, nil, cfg, nil)
			t.Cleanup(billing.Stop)
			openAI := &OpenAIGatewayHandler{gatewayService: &service.OpenAIGatewayService{}, apiKeyService: &service.APIKeyService{}, concurrencyHelper: newGatewayAdmissionForTest().concurrencyHelper, billingCacheService: billing, securityAuditCoordinator: coordinator}
			gateway := &GatewayHandler{concurrencyHelper: newGatewayAdmissionForTest().concurrencyHelper, gatewayService: &service.GatewayService{}, billingCacheService: billing, securityAuditCoordinator: coordinator}
			groupID := int64(3)
			public := endpoint
			if endpoint == "web_search" {
				public = "grok-web-search"
			}
			if endpoint == "x_search" {
				public = "grok-x-search"
			}
			key := &service.APIKey{ID: 9, UserID: 7, User: &service.User{ID: 7}, GroupID: &groupID, Group: &service.Group{ID: groupID, Platform: service.PlatformComposite, RateMultiplier: 2, BillingRatePolicy: domain.BillingRatePolicy{PlatformMultipliers: map[string]float64{service.PlatformGrok: 3}, ModelMultipliers: []domain.BillingModelRate{{Platform: service.PlatformGrok, Model: public, Multiplier: 4}}}}}
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/"+endpoint, strings.NewReader(`{"input":"allowed speech","query":"allowed query"}`))
			c.Request.Header.Set("Content-Type", "application/json")
			c.Set(string(middleware2.ContextKeyAPIKey), key)
			c.Set(string(middleware2.ContextKeyUser), middleware2.AuthSubject{UserID: 7, Concurrency: 1})
			switch endpoint {
			case "web_search":
				gateway.WebSearch(c)
			case "x_search":
				gateway.XSearch(c)
			default:
				openAI.GrokVoice(c, endpoint)
			}
			require.Contains(t, recorder.Body.String(), "balance", "accepted audit must reach authoritative billing before any account can be selected")
			require.Equal(t, int64(1), engine.calls.Load())
			raw, err := service.ExportBillingPricingSnapshot(c.Request.Context())
			require.NoError(t, err)
			var stored struct {
				Platform    string `json:"platform"`
				PublicModel string `json:"public_model"`
			}
			require.NoError(t, json.Unmarshal(raw, &stored))
			require.Equal(t, service.PlatformGrok, stored.Platform)
			require.Equal(t, public, stored.PublicModel)
			require.Equal(t, 0, cache.count(), "denied eligibility must not reserve or consume quota")
		})
	}
}
