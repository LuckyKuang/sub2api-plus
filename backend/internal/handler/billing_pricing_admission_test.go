//go:build unit || !integration

package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
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

type pricingAdmissionEngine struct {
	kind             securityaudit.DecisionKind
	calls            atomic.Int64
	observedSnapshot atomic.Bool
}

func (*pricingAdmissionEngine) EffectiveMode() securityaudit.Mode                    { return securityaudit.ModeBlocking }
func (*pricingAdmissionEngine) Enqueue(context.Context, securityaudit.Request) error { return nil }
func (engine *pricingAdmissionEngine) Evaluate(ctx context.Context, _ securityaudit.Request) (*securityaudit.PromptDecision, error) {
	engine.calls.Add(1)
	if service.BillingPricingSnapshotFromContext(ctx) != nil {
		engine.observedSnapshot.Store(true)
	}
	return &securityaudit.PromptDecision{Kind: engine.kind, AllowNextStage: engine.kind == securityaudit.DecisionAllow}, nil
}

func TestPricingSnapshotFreezesOnlyAfterSuccessfulIngressAudit(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, family := range []string{"gateway", "openai"} {
		for _, kind := range []securityaudit.DecisionKind{securityaudit.DecisionAllow, securityaudit.DecisionBlock, securityaudit.DecisionUnavailable} {
			t.Run(family+"/"+string(kind), func(t *testing.T) {
				engine := &pricingAdmissionEngine{kind: kind}
				coordinator := securityaudit.NewCoordinator(nil, engine)
				c, _ := gin.CreateTestContext(httptest.NewRecorder())
				c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
				key := &service.APIKey{Group: &service.Group{ID: 3, Platform: service.PlatformOpenAI, RateMultiplier: 1}}
				subject := middleware2.AuthSubject{UserID: 7}
				var check func() *securityaudit.Decision
				if family == "gateway" {
					h := &GatewayHandler{gatewayService: &service.GatewayService{}, securityAuditCoordinator: coordinator}
					check = func() *securityaudit.Decision {
						return h.checkSecurityAudit(c, nil, key, subject, service.ContentModerationProtocolOpenAIChat, "public-model", []byte(`{"messages":[{"role":"user","content":"audit me"}]}`))
					}
				} else {
					h := &OpenAIGatewayHandler{gatewayService: &service.OpenAIGatewayService{}, securityAuditCoordinator: coordinator}
					check = func() *securityaudit.Decision {
						return h.checkSecurityAudit(c, nil, key, subject, service.ContentModerationProtocolOpenAIChat, "public-model", []byte(`{"messages":[{"role":"user","content":"audit me"}]}`))
					}
				}
				decision := check()
				require.NotNil(t, decision)
				require.False(t, engine.observedSnapshot.Load(), "group/personal/channel pricing must not be fetched before the audit permits it")
				snapshot := service.BillingPricingSnapshotFromContext(c.Request.Context())
				if kind != securityaudit.DecisionAllow {
					require.False(t, decision.AllowNextStage)
					require.Nil(t, snapshot, "audit rejection must leave billing capture untouched")
					return
				}
				require.True(t, decision.AllowNextStage)
				require.NotNil(t, snapshot, "successful admission freezes billing before downstream work")
				require.False(t, snapshot.PricingAt().IsZero())
				key.Group.RateMultiplier = 9
				require.Nil(t, check(), "the same HTTP request reuses its successful audit")
				require.Same(t, snapshot, service.BillingPricingSnapshotFromContext(c.Request.Context()), "retries must keep the original pricing generation")
				require.Equal(t, int64(1), engine.calls.Load())
			})
		}
	}
}

func TestDetachedUsageRetainsExactPricingSnapshotAfterRequestCancellation(t *testing.T) {
	for _, family := range []string{"gateway", "openai"} {
		for _, pooled := range []bool{false, true} {
			name := family + "/fallback"
			if pooled {
				name = family + "/pool"
			}
			t.Run(name, func(t *testing.T) {
				key := &service.APIKey{Group: &service.Group{ID: 3, Platform: service.PlatformOpenAI, RateMultiplier: 1}}
				parent, cancel := context.WithCancel((&service.OpenAIGatewayService{}).WithBillingPricingSnapshot(context.Background(), key, time.Now(), "public-model"))
				snapshot := service.BillingPricingSnapshotFromContext(parent)
				require.NotNil(t, snapshot)
				cancel()
				var pool *service.UsageRecordWorkerPool
				if pooled {
					pool = newUsageRecordTestPool(t)
				}
				var submit func(context.Context, service.UsageRecordTask)
				if family == "gateway" {
					submit = (&GatewayHandler{usageRecordWorkerPool: pool}).submitUsageRecordTask
				} else {
					submit = (&OpenAIGatewayHandler{usageRecordWorkerPool: pool}).submitMandatoryUsageRecordTask
				}
				type observed struct {
					snapshot *service.BillingPricingSnapshot
					err      error
				}
				completed := make(chan observed, 1)
				submit(parent, func(ctx context.Context) {
					completed <- observed{service.BillingPricingSnapshotFromContext(ctx), ctx.Err()}
				})
				select {
				case actual := <-completed:
					require.Same(t, snapshot, actual.snapshot, "worker/fallback context must preserve the accepted pricing generation")
					require.NoError(t, actual.err, "client cancellation must not cancel mandatory settlement")
				case <-time.After(2 * time.Second):
					t.Fatal("settlement task did not execute")
				}
			})
		}
	}
}

func TestGrokVoiceAndRealtimeRejectBeforeBillingOrProviderSideEffects(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, endpoint := range []string{"tts", "stt", "custom-voices", "realtime"} {
		t.Run(endpoint, func(t *testing.T) {
			engine := blockingHandlerPromptEngine()
			cfg := &config.Config{}
			billing := service.NewBillingCacheService(nil, nil, nil, nil, nil, nil, cfg, nil)
			t.Cleanup(billing.Stop)
			h := &OpenAIGatewayHandler{gatewayService: &service.OpenAIGatewayService{}, apiKeyService: &service.APIKeyService{}, concurrencyHelper: NewConcurrencyHelper(service.NewConcurrencyService(&auditInflightConcurrencyCache{}), SSEPingFormatNone, 0), billingCacheService: billing, securityAuditCoordinator: securityaudit.NewCoordinator(nil, engine)}
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/audio/"+endpoint, strings.NewReader(`{"input":"blocked speech"}`))
			if endpoint == "realtime" {
				c.Request = httptest.NewRequest(http.MethodGet, "/v1/realtime", nil)
				c.Request.Header.Set("Connection", "Upgrade")
				c.Request.Header.Set("Upgrade", "websocket")
			}
			groupID := int64(3)
			key := &service.APIKey{ID: 9, UserID: 7, User: &service.User{ID: 7}, GroupID: &groupID, Group: &service.Group{ID: 3, Platform: service.PlatformGrok}}
			c.Set(string(middleware2.ContextKeyAPIKey), key)
			c.Set(string(middleware2.ContextKeyUser), middleware2.AuthSubject{UserID: 7, Concurrency: 1})
			if endpoint == "realtime" {
				h.GrokRealtime(c)
			} else {
				h.GrokVoice(c, endpoint)
			}
			require.Equal(t, http.StatusForbidden, recorder.Code)
			require.Contains(t, recorder.Body.String(), securityaudit.ErrorCodeBlocked)
			require.Nil(t, service.BillingPricingSnapshotFromContext(c.Request.Context()))
			evaluated, _, requests := engine.snapshot()
			require.Equal(t, 1, evaluated)
			require.Len(t, requests, 1)
			if endpoint == "tts" {
				require.Equal(t, "tts", requests[0].Model, "pricing identity is the billed audio model, not the text account selector")
			}
		})
	}
}

func TestGrokRealtimeFrameAuditDoesNotReuseHTTPCompletion(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := blockingHandlerPromptEngine()
	h := &OpenAIGatewayHandler{securityAuditCoordinator: securityaudit.NewCoordinator(nil, engine)}
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodGet, "/v1/realtime", nil)
	c.Set(securityAuditCompletedContextKey, true)
	check := h.grokRealtimeClientAudit(c, nil, nil, middleware2.AuthSubject{UserID: 7}, "grok-voice-latest", nil)
	for _, body := range []string{
		`{"type":"conversation.item.create","item":{"type":"message","role":"user","content":[{"type":"input_text","text":"first turn"}]}}`,
		`{"type":"conversation.item.create","item":{"type":"message","role":"user","content":[{"type":"input_text","text":"second turn"}]},"future_sibling":true}`,
	} {
		require.Error(t, check(context.Background(), []byte(body)))
	}
	evaluated, _, requests := engine.snapshot()
	require.Equal(t, 2, evaluated, "every frame must be independently admitted even after a successful HTTP gate")
	require.Len(t, requests, 2)
	for _, request := range requests {
		require.Equal(t, "grok_realtime", request.Stage)
	}
}

func TestGrokRealtimeCanonicalAuditPermitsUnknownAudioAndRetainsUserSiblings(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := securityaudit.NewPromptService(blockingCompatibilityConfigStore{cfg: securityaudit.ActiveConfig{
		RiskControlEnabled: true, Enabled: true, BlockingEnabled: true, AllGroups: true,
		Scanners: []string{"pii"}, Endpoints: []securityaudit.ActiveEndpoint{{ID: "guard", Enabled: true, TimeoutMS: 1000, InputLimit: 4096}},
	}}, nil, nil, nil, securityaudit.NewAtomicMetrics())
	h := &OpenAIGatewayHandler{securityAuditCoordinator: securityaudit.NewCoordinator(nil, engine)}
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodGet, "/v1/realtime", nil)
	check := h.grokRealtimeClientAudit(c, nil, nil, middleware2.AuthSubject{UserID: 7}, "grok-voice-latest", nil)
	for _, body := range []string{`{"type":"future.audio.control","payload":{"opaque":true}}`, `{"type":"input_audio_buffer.append","audio":"QklOQVJZ"}`, `{"type":"response.create"}`} {
		require.NoError(t, check(context.Background(), []byte(body)), "unextractable content must pass through the real audit engine")
	}
	body := []byte(`{"type":"conversation.item.create","item":{"type":"message","role":"user","content":[{"type":"input_text","text":"recognized user sibling"},{"type":"future_part","opaque":true}]},"future_sibling":true}`)
	moderation := service.ExtractContentModerationInput(service.ContentModerationProtocolOpenAILive, body)
	require.Equal(t, "recognized user sibling", moderation.Text)
	prompt, err := securityaudit.ExtractPromptSnapshot(securityaudit.Request{Protocol: service.ContentModerationProtocolOpenAILive, Body: body})
	require.NoError(t, err)
	require.Equal(t, "recognized user sibling", prompt.ScanText)
}

func TestEmptyMediaSubmissionsStillEnterAuditAndFreezePricing(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, endpoint := range []string{"async-image", "batch-image"} {
		t.Run(endpoint, func(t *testing.T) {
			engine := &pricingAdmissionEngine{kind: securityaudit.DecisionAllow}
			openAI := &OpenAIGatewayHandler{gatewayService: &service.OpenAIGatewayService{}, securityAuditCoordinator: securityaudit.NewCoordinator(nil, engine)}
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/images/generations", nil)
			c.Request.Header.Set("Content-Type", "application/json")
			key := &service.APIKey{ID: 9, UserID: 7, Group: &service.Group{ID: 3, Platform: service.PlatformGrok, RateMultiplier: 1}}
			c.Set(string(middleware2.ContextKeyAPIKey), key)
			c.Set(string(middleware2.ContextKeyUser), middleware2.AuthSubject{UserID: 7})
			if endpoint == "async-image" {
				h := &AsyncImageHandler{openAI: openAI}
				require.True(t, h.checkSecurityAuditBeforeSubmit(c, key, service.PlatformGrok, []byte(`{"model":"grok-imagine-image","future_media":{"opaque":true}}`)))
			} else {
				h := &BatchImageHandler{openAI: openAI}
				require.True(t, h.checkSecurityAuditBeforeSubmit(c, &service.BatchImageSubmitRequest{Model: "grok-imagine-image"}))
			}
			require.Equal(t, int64(1), engine.calls.Load(), "empty extracted content cannot fabricate a completed audit")
			require.False(t, engine.observedSnapshot.Load())
			require.NotNil(t, service.BillingPricingSnapshotFromContext(c.Request.Context()))
		})
	}
}
