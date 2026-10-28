//go:build unit

package handler

import (
	"encoding/json"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/domain"
	"github.com/LuckyKuang/sub2api-plus/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// Requirement: one Composite key can create, extend, reference and delete an
// already-supported Gemini cache. Routing aliases are public billing identities;
// only the selected Gemini model is sent upstream. The independent dollar
// examples use $1.25 input and $4.50 per hour for one million cached tokens.
func TestGeminiCachedContentsCompositeAliasLifecycleChargesPublicRules(t *testing.T) {
	env := newCachedBillingEnv(t, 10000, 900)
	env.group.Platform = service.PlatformComposite
	env.group.RateMultiplier = .8
	env.group.BillingPolicyVersion = 14
	env.group.BillingRatePolicy = domain.BillingRatePolicy{
		PlatformMultipliers: map[string]float64{service.PlatformGemini: 9, service.PlatformOpenAI: 7},
		ModelMultipliers: []domain.BillingModelRate{
			{Platform: service.PlatformGemini, Model: "premium-cache", BillingUnit: "token", Multiplier: 2},
			{Platform: service.PlatformGemini, Model: "premium-cache", BillingUnit: "hour", Multiplier: 4},
		},
	}
	decision := service.CompositeRouteDecision{Matched: true, PublicModel: "premium-cache", UpstreamModel: "gemini-2.5-pro", TargetPlatform: service.PlatformGemini}
	env.upstream.createBody = cachedBillingCreateBody(time.Now().Add(100 * time.Hour))
	var sentModel string
	env.upstream.onRequest = func(req *http.Request) {
		if req.Method == http.MethodPost {
			body, err := io.ReadAll(req.Body)
			require.NoError(t, err)
			var payload struct {
				Model string `json:"model"`
			}
			require.NoError(t, json.Unmarshal(body, &payload))
			sentModel = payload.Model
		}
	}
	rec := env.serve(http.MethodPost, "/v1beta/cachedContents",
		`{"model":"models/premium-cache","ttl":"86400s","contents":[{"role":"user","parts":[{"text":"doc"}]}]}`,
		nil, func(c *gin.Context) {
			c.Request = c.Request.WithContext(service.WithCompositeRouteDecision(c.Request.Context(), decision))
			env.h.GeminiCachedContentsCreate(c)
		})
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Equal(t, "models/gemini-2.5-pro", sentModel)
	require.Contains(t, rec.Body.String(), "models/premium-cache")
	require.NotContains(t, rec.Body.String(), "gemini-2.5-pro")
	env.waitForUsage(t, 1)
	usage := env.usage.first()
	// $1.25 × .8 × 9 × 2 + ($4.50 × 24 hours) × .8 × 9 × 4 = $3128.40.
	require.InDelta(t, 3128.4, usage.ActualCost, 1e-9)
	require.EqualValues(t, 14, usage.BillingPolicyVersion)
	require.Len(t, env.repo.records, 1)
	var stored *service.GeminiCachedContent
	for _, record := range env.repo.records {
		stored = record
	}
	require.Equal(t, int64(900), stored.AccountID)
	require.Equal(t, "premium-cache", stored.ChannelUsage.OriginalModel)
	require.Equal(t, "gemini-2.5-pro", stored.Model)

	c, _ := newGeminiCachedContentTestContext(http.MethodPost, "/v1beta/models/premium-cache:generateContent", env.apiKey)
	c.Request = c.Request.WithContext(service.WithCompositeRouteDecision(c.Request.Context(), decision))
	bound, body, ok := env.h.resolveGeminiCachedContentBinding(c, zap.NewNop(), env.apiKey, "gemini-2.5-pro",
		[]byte(`{"cachedContent":"`+stored.PublicName()+`","contents":[]}`))
	require.True(t, ok)
	require.Equal(t, int64(900), bound.AccountID)
	require.JSONEq(t, `{"cachedContent":"cachedContents/up1","contents":[]}`, string(body))

	// A different public alias must not reuse this binding even when both route
	// to the same private model; the documented literal-model rule still applies.
	other := decision
	other.PublicModel = "other-cache"
	c, mismatch := newGeminiCachedContentTestContext(http.MethodPost, "/v1beta/models/other-cache:generateContent", env.apiKey)
	c.Request = c.Request.WithContext(service.WithCompositeRouteDecision(c.Request.Context(), other))
	_, _, ok = env.h.resolveGeminiCachedContentBinding(c, zap.NewNop(), env.apiKey, "gemini-2.5-pro",
		[]byte(`{"cachedContent":"`+stored.PublicName()+`","contents":[]}`))
	require.False(t, ok)
	require.Equal(t, http.StatusBadRequest, mismatch.Code)
	require.NotContains(t, mismatch.Body.String(), "gemini-2.5-pro")

	// A resource operation has no model in its path. Its authorized record and
	// original public-model evidence bind the extension pricing, never its ID.
	stored.ExpireTime = time.Now().Add(time.Hour)
	rec = env.serve(http.MethodPatch, "/v1beta/cachedContents/"+stored.PublicID+"?updateMask=ttl", `{"ttl":"86400s"}`,
		gin.Params{{Key: "cacheID", Value: stored.PublicID}}, func(c *gin.Context) {
			c.Request = c.Request.WithContext(service.WithResolvedTargetPlatform(c.Request.Context(), service.PlatformGemini))
			env.h.GeminiCachedContentsPatch(c)
		})
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	env.waitForUsage(t, 2)
	env.usage.mu.Lock()
	extensionCost := env.usage.logs[1].ActualCost
	env.usage.mu.Unlock()
	// 23 new hours × $4.50 × .8 × 9 × 4 = $2980.80; a small wall-clock
	// allowance accounts for the elapsed time between setting and reading TTL.
	require.InDelta(t, 2980.8, extensionCost, .01)
	require.Equal(t, 1, env.upstream.methodCalls(http.MethodPatch))

	rec = env.serve(http.MethodDelete, "/v1beta/cachedContents/"+stored.PublicID, "",
		gin.Params{{Key: "cacheID", Value: stored.PublicID}}, func(c *gin.Context) {
			c.Request = c.Request.WithContext(service.WithResolvedTargetPlatform(c.Request.Context(), service.PlatformGemini))
			env.h.GeminiCachedContentsDelete(c)
		})
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Equal(t, 1, env.upstream.methodCalls(http.MethodDelete))
	require.Len(t, env.usage.totals(), 2, "deletion must not invent a new charge or refund")
}

func TestGeminiCachedContentsCompositeWrongSupplierRejectedBeforeCreate(t *testing.T) {
	for _, target := range []string{service.PlatformOpenAI, service.PlatformAntigravity} {
		t.Run(target, func(t *testing.T) {
			env := newCachedBillingEnv(t, 1000, 900)
			env.group.Platform = service.PlatformComposite
			rec := env.serve(http.MethodPost, "/v1beta/cachedContents",
				`{"model":"models/cache-alias","contents":[]}`, nil, func(c *gin.Context) {
					c.Request = c.Request.WithContext(service.WithResolvedTargetPlatform(c.Request.Context(), target))
					env.h.GeminiCachedContentsCreate(c)
				})
			require.Equal(t, http.StatusBadRequest, rec.Code)
			require.Empty(t, env.repo.records)
			require.Empty(t, env.usage.totals())
			require.Empty(t, env.upstream.calls)
		})
	}
}
