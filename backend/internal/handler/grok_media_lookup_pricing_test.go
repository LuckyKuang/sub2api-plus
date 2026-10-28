//go:build unit

package handler

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/domain"
	"github.com/LuckyKuang/sub2api-plus/internal/securityaudit"
	middleware "github.com/LuckyKuang/sub2api-plus/internal/server/middleware"
	"github.com/LuckyKuang/sub2api-plus/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

type mediaLookupPersonalRateRepo struct {
	service.UserGroupRateRepository
	err   error
	calls int
}

func (r *mediaLookupPersonalRateRepo) GetByUserAndGroup(context.Context, int64, int64) (*float64, error) {
	r.calls++
	return nil, r.err
}

// Requirement: retrieving an accepted task uses its create-time price evidence,
// not today's admission quote. This holds after policy changes, a price-repo
// outage, missing evidence after prior settlement, and legacy task records.
func TestMediaTaskLookupsUseOwnedPricingBeforeCurrentDependencies(t *testing.T) {
	for _, endpoint := range []string{"grok-status", "grok-content", "seedance-status", "seedance-delete"} {
		for _, evidence := range []string{"valid", "missing", "corrupt", "legacy"} {
			for _, change := range []string{"rate-overflow", "personal-repo-outage"} {
				t.Run(endpoint+"/"+evidence+"/"+change, func(t *testing.T) {
					rates := &mediaLookupPersonalRateRepo{}
					platform, model, task := service.PlatformGrok, "grok-imagine-video", "task"
					platforms := []string{}
					if strings.HasPrefix(endpoint, "seedance") {
						platform, model, task = service.PlatformOpenAI, "doubao-seedance", "seedance:task"
						platforms = []string{platform}
					}
					h, slots, bindings, upstream := newGrokMediaSlotHandlerWithRates(t, false, false, rates, platforms...)
					engine := &pricingAdmissionEngine{kind: securityaudit.DecisionAllow}
					h.securityAuditCoordinator = securityaudit.NewCoordinator(nil, engine)
					c, response := grokMediaSlotContext(context.Background(), false)
					key, _ := middleware.GetAPIKeyFromContext(c)
					key.Group.Platform = service.PlatformComposite
					key.Group.RateMultiplier = 1
					key.Group.BillingPolicyVersion = 7
					key.Group.BillingRatePolicy = domain.BillingRatePolicy{PlatformMultipliers: map[string]float64{platform: 2}}
					if platform == service.PlatformOpenAI {
						key.Group.ModelPricing = seedanceTokenPriceCard(2e-6)
					}
					at := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
					created := h.gatewayService.WithBillingPricingSnapshot(service.WithResolvedTargetPlatform(context.Background(), platform), key, at, model)
					require.NoError(t, service.ValidateBillingPricingSnapshot(created))
					pending := service.GrokVideoPendingBilling{Model: model, BillingModel: model, OriginalModel: model, VideoDurationSeconds: 6, VideoResolution: "720p"}
					if evidence != "missing" {
						storedCtx := created
						if evidence == "legacy" {
							storedCtx = context.Background()
						}
						if evidence == "corrupt" {
							pending.PricingSnapshot = json.RawMessage(`{"version":-1}`)
						}
						require.NoError(t, h.gatewayService.StoreGrokVideoPendingBilling(storedCtx, task, 10, key.ID, pending))
					}
					require.NoError(t, h.gatewayService.BindGrokMediaVideoRequestAccount(context.Background(), key.GroupID, task, 10, key.ID, 1))
					initialReads := rates.calls
					if change == "rate-overflow" {
						key.Group.RateMultiplier = 2
						key.Group.BillingRatePolicy.PlatformMultipliers[platform] = 600000
					} else {
						rates.err = errors.New("personal pricing repository down")
					}
					key.Group.BillingPolicyVersion = 8
					upstream.call = func(req *http.Request, _ int64) (*http.Response, error) {
						body := `{"request_id":"task","status":"pending"}`
						if platform == service.PlatformOpenAI {
							body = `{"id":"task","status":"running"}`
						}
						return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(body))}, nil
					}
					method := http.MethodGet
					if endpoint == "seedance-delete" {
						method = http.MethodDelete
					}
					c.Request = httptest.NewRequest(method, "/v1/videos/task", nil)
					if strings.HasPrefix(endpoint, "seedance") {
						c.Request = httptest.NewRequest(method, "/api/v3/contents/generations/tasks/task", nil)
						c.Params = gin.Params{{Key: "task_id", Value: "task"}}
						h.SeedanceTasks(c)
					} else if endpoint == "grok-content" {
						h.GrokVideoContent(c)
					} else {
						h.GrokVideoStatus(c)
					}
					require.Equal(t, http.StatusOK, response.Code, response.Body.String())
					require.Equal(t, int64(1), engine.calls.Load(), "every lookup still enters canonical audit")
					require.Equal(t, initialReads, rates.calls, "lookup must never capture today's personal price")
					expectedCalls := 1
					if endpoint == "grok-content" {
						expectedCalls = 2
					}
					require.Equal(t, expectedCalls, upstream.calls)
					require.Empty(t, bindings.billed, "unfinished lookup or delete cannot claim settlement")
					slots.assertReleased(t)
					if evidence == "valid" {
						snapshot := service.BillingPricingSnapshotFromContext(c.Request.Context())
						require.NotNil(t, snapshot)
						require.Equal(t, at, snapshot.PricingAt())
						raw, err := service.ExportBillingPricingSnapshot(c.Request.Context(), model)
						require.NoError(t, err)
						var captured struct {
							Group    service.Group
							Platform string
						}
						require.NoError(t, json.Unmarshal(raw, &captured))
						require.Equal(t, platform, captured.Platform)
						require.EqualValues(t, 7, captured.Group.BillingPolicyVersion)
						require.Equal(t, 2.0, captured.Group.BillingRatePolicy.PlatformMultipliers[platform])
					} else {
						require.Nil(t, service.BillingPricingSnapshotFromContext(c.Request.Context()), "unknown and legacy evidence cannot become a new admission quote")
					}
					// Independent completion boundary: missing or corrupt evidence
					// leaves its claim untouched, allowing later storage repair.
					if evidence == "missing" || evidence == "corrupt" {
						subject := middleware.AuthSubject{UserID: 10}
						if platform == service.PlatformOpenAI {
							_, bill := prepareSeedanceCompletionBilling(c.Request.Context(), h, key, subject, task, &service.OpenAIForwardResult{Usage: service.OpenAIUsage{OutputTokens: 12345}})
							require.Nil(t, bill)
						} else {
							_, bill := prepareGrokVideoCompletionBilling(c.Request.Context(), h, zap.NewNop(), key, subject, task, &service.OpenAIForwardResult{VideoCount: 1})
							require.Nil(t, bill)
						}
						require.Empty(t, bindings.billed)
					}
				})
			}
		}
	}
}

func TestMediaTaskLookupRejectsUnownedResourceBeforePricingImport(t *testing.T) {
	for _, denial := range []string{"wrong-group", "wrong-user", "wrong-key", "audit-block"} {
		t.Run(denial, func(t *testing.T) {
			h, slots, bindings, upstream := newGrokMediaSlotHandler(t, false, false)
			c, response := grokMediaSlotContext(context.Background(), false)
			key, _ := middleware.GetAPIKeyFromContext(c)
			key.Group.RateMultiplier = 1
			created := h.gatewayService.WithBillingPricingSnapshot(context.Background(), key, time.Now(), "grok-imagine-video")
			require.NoError(t, h.gatewayService.StoreGrokVideoPendingBilling(created, "task", 10, key.ID, service.GrokVideoPendingBilling{Model: "grok-imagine-video"}))
			kind := securityaudit.DecisionAllow
			switch denial {
			case "wrong-group":
				id := int64(25)
				key.GroupID = &id
			case "wrong-user":
				c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: 99, Concurrency: 5})
			case "wrong-key":
				key.ID = 99
			case "audit-block":
				kind = securityaudit.DecisionBlock
			}
			engine := &pricingAdmissionEngine{kind: kind}
			h.securityAuditCoordinator = securityaudit.NewCoordinator(nil, engine)
			h.GrokVideoStatus(c)
			expected := http.StatusNotFound
			if denial == "audit-block" {
				expected = http.StatusForbidden
			}
			require.Equal(t, expected, response.Code, response.Body.String())
			require.Equal(t, int64(1), engine.calls.Load())
			require.Zero(t, bindings.pendingReads, "unauthorized resources cannot import another task's pricing")
			require.Nil(t, service.BillingPricingSnapshotFromContext(c.Request.Context()))
			require.Zero(t, slots.userAcquired)
			require.Zero(t, slots.acquired)
			require.Zero(t, upstream.calls)
			require.Empty(t, bindings.billed)
		})
	}
}
