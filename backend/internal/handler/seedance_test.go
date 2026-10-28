//go:build unit

package handler

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/config"
	"github.com/LuckyKuang/sub2api-plus/internal/domain"
	"github.com/LuckyKuang/sub2api-plus/internal/securityaudit"
	middleware "github.com/LuckyKuang/sub2api-plus/internal/server/middleware"
	"github.com/LuckyKuang/sub2api-plus/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestSeedanceHandlerLifecycleAndOwnership(t *testing.T) {
	h, slots, bindings, upstream := newGrokMediaSlotHandler(t, false, false, service.PlatformOpenAI)
	var owner int64
	upstream.call = func(req *http.Request, id int64) (*http.Response, error) {
		body := `{"id":"task-ark","status":"queued"}`
		if req.Method == http.MethodPost {
			owner = id
		} else {
			require.Equal(t, owner, id)
		}
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(body))}, nil
	}
	newContext := func(method string) (*gin.Context, *httptest.ResponseRecorder) {
		c, w := grokMediaSlotContext(context.Background(), method == http.MethodPost)
		key, _ := middleware.GetAPIKeyFromContext(c)
		key.Group.Platform = service.PlatformOpenAI
		key.Group.ModelPricing = seedanceTokenPriceCard(2e-6)
		body := ""
		if method == http.MethodPost {
			body = `{"model":"doubao-seedance","content":[{"type":"text","text":"waves"}]}`
		}
		c.Request = httptest.NewRequest(method, "/api/v3/contents/generations/tasks", strings.NewReader(body))
		c.Params = gin.Params{{Key: "task_id", Value: "task-ark"}}
		return c, w
	}
	c, w := newContext(http.MethodPost)
	h.SeedanceTasks(c)
	require.Equal(t, 200, w.Code, w.Body.String())
	require.Positive(t, owner)
	require.Len(t, bindings.pending, 1)
	slots.assertReleased(t)
	for _, method := range []string{http.MethodGet, http.MethodDelete} {
		c, w = newContext(method)
		h.SeedanceTasks(c)
		require.Equal(t, 200, w.Code, w.Body.String())
		slots.assertReleased(t)
	}
	for _, other := range []string{"user", "key", "group", "task", "provider"} {
		c, w = newContext(http.MethodGet)
		key, _ := middleware.GetAPIKeyFromContext(c)
		switch other {
		case "user":
			c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: 11, Concurrency: 5})
		case "key":
			key.ID = 21
		case "group":
			group := int64(25)
			key.GroupID = &group
		case "task":
			c.Params = gin.Params{{Key: "task_id", Value: "other"}}
		case "provider":
			c.Params = gin.Params{{Key: "request_id", Value: "task-ark"}}
		}
		before := upstream.calls
		if other == "provider" {
			h.GrokVideoStatus(c)
		} else {
			h.SeedanceTasks(c)
		}
		require.Equal(t, 404, w.Code, other+": "+w.Body.String())
		require.Equal(t, before, upstream.calls)
		slots.assertReleased(t)
	}
	c, _ = newContext(http.MethodGet)
	key, _ := middleware.GetAPIKeyFromContext(c)
	subject, _ := middleware.GetAuthSubjectFromContext(c)
	result := &service.OpenAIForwardResult{Usage: service.OpenAIUsage{OutputTokens: 12345}, ResponseID: "seedance:task-ark"}
	for i := range 20 {
		_, billed := prepareSeedanceCompletionBilling(context.Background(), h, key, subject, result.ResponseID, result)
		if i == 0 {
			require.NotNil(t, billed)
			require.Equal(t, "doubao-seedance", billed.BillingModel)
			require.Equal(t, 12345, billed.Usage.OutputTokens)
			require.Zero(t, billed.VideoCount)
		} else {
			require.Nil(t, billed)
		}
	}
	require.Len(t, bindings.billed, 1)
}

func TestSeedanceCompletionRestoresAdmittedPricingBeforeClaim(t *testing.T) {
	h, _, bindings, _ := newGrokMediaSlotHandler(t, false, false, service.PlatformOpenAI)
	c, _ := grokMediaSlotContext(context.Background(), false)
	key, ok := middleware.GetAPIKeyFromContext(c)
	require.True(t, ok)
	key.Group.Platform, key.Group.BillingPolicyVersion = service.PlatformOpenAI, 12
	key.Group.ModelPricing = seedanceTokenPriceCard(2e-6)
	at := time.Date(2026, 1, 1, 1, 0, 0, 0, time.UTC)
	createCtx := h.gatewayService.WithBillingPricingSnapshot(context.Background(), key, at, "public-seedance")
	subject := middleware.AuthSubject{UserID: 10}
	pending := service.GrokVideoPendingBilling{Model: "public-seedance", OriginalModel: "public-seedance", UpstreamModel: "doubao-seedance"}
	require.NoError(t, h.gatewayService.StoreGrokVideoPendingBilling(createCtx, "seedance:task", subject.UserID, key.ID, pending))
	key.Group.BillingPolicyVersion = 13
	pollCtx := h.gatewayService.WithBillingPricingSnapshot(context.Background(), key, at.Add(time.Hour), "different-model")
	result := &service.OpenAIForwardResult{Usage: service.OpenAIUsage{OutputTokens: 12345}, ResponseID: "seedance:task"}
	ctx, bill := prepareSeedanceCompletionBilling(pollCtx, h, key, subject, result.ResponseID, result)
	require.NotNil(t, bill)
	require.Equal(t, at, service.BillingPricingSnapshotFromContext(ctx).PricingAt())
	data, err := service.ExportBillingPricingSnapshot(ctx, "doubao-seedance")
	require.NoError(t, err)
	var restored struct {
		Group    service.Group
		Platform string
	}
	require.NoError(t, json.Unmarshal(data, &restored))
	require.EqualValues(t, 12, restored.Group.BillingPolicyVersion)
	require.Equal(t, service.PlatformOpenAI, restored.Platform)
	require.Len(t, bindings.billed, 1)

	pending.PricingSnapshot = json.RawMessage(`{"version":-1}`)
	require.NoError(t, h.gatewayService.StoreGrokVideoPendingBilling(createCtx, "seedance:corrupt", subject.UserID, key.ID, pending))
	_, bill = prepareSeedanceCompletionBilling(pollCtx, h, key, subject, "seedance:corrupt", result)
	require.Nil(t, bill)
	require.Len(t, bindings.billed, 1, "invalid persisted prices must not consume the corrupt task's settlement claim")
}

func seedanceTokenPriceCard(output float64) []service.ChannelModelPricing {
	input := 0.0
	return []service.ChannelModelPricing{{Platform: service.PlatformOpenAI, Models: []string{"doubao-seedance"}, BillingMode: service.BillingModeToken, InputPrice: &input, OutputPrice: &output}}
}

func TestSeedanceMissingTokenPricePreservesClaimForRecoveredSnapshot(t *testing.T) {
	h, _, bindings, _ := newGrokMediaSlotHandler(t, false, false, service.PlatformOpenAI)
	c, _ := grokMediaSlotContext(context.Background(), false)
	key, _ := middleware.GetAPIKeyFromContext(c)
	key.Group.Platform = service.PlatformOpenAI
	subject := middleware.AuthSubject{UserID: 10}
	pending := service.GrokVideoPendingBilling{Model: "public-seedance", OriginalModel: "public-seedance", UpstreamModel: "doubao-seedance"}
	create := h.gatewayService.WithBillingPricingSnapshot(context.Background(), key, time.Now(), "public-seedance")
	require.NoError(t, h.gatewayService.StoreGrokVideoPendingBilling(create, "seedance:recover-price", subject.UserID, key.ID, pending))
	result := &service.OpenAIForwardResult{ResponseID: "seedance:recover-price", Usage: service.OpenAIUsage{OutputTokens: 12345}}
	_, bill := prepareSeedanceCompletionBilling(context.Background(), h, key, subject, result.ResponseID, result)
	require.Nil(t, bill)
	require.Empty(t, bindings.billed, "missing token prices cannot consume the claim or produce a billing job")
	for _, name := range []string{"empty_token_card", "input_only_token_card"} {
		t.Run(name, func(t *testing.T) {
			key.Group.ModelPricing = []service.ChannelModelPricing{{Platform: service.PlatformOpenAI, Models: []string{"doubao-seedance"}, BillingMode: service.BillingModeToken}}
			if name == "input_only_token_card" {
				input := 1e-6
				key.Group.ModelPricing[0].InputPrice = &input
			}
			create := h.gatewayService.WithBillingPricingSnapshot(context.Background(), key, time.Now(), "public-seedance")
			require.NoError(t, h.gatewayService.StoreGrokVideoPendingBilling(create, result.ResponseID, subject.UserID, key.ID, pending))
			_, bill := prepareSeedanceCompletionBilling(context.Background(), h, key, subject, result.ResponseID, result)
			require.Nil(t, bill)
			require.Empty(t, bindings.billed, "an absent output price is not an explicit free output price")
		})
	}
	// Repair stored evidence using an actual explicit zero token card. Zero is a
	// configured free price; it must remain distinguishable from the missing card.
	key.Group.ModelPricing = seedanceTokenPriceCard(0)
	create = h.gatewayService.WithBillingPricingSnapshot(context.Background(), key, time.Now(), "public-seedance")
	require.NoError(t, h.gatewayService.StoreGrokVideoPendingBilling(create, result.ResponseID, subject.UserID, key.ID, pending))
	ctx, bill := prepareSeedanceCompletionBilling(context.Background(), h, key, subject, result.ResponseID, result)
	require.NotNil(t, bill)
	require.Len(t, bindings.billed, 1)
	require.NoError(t, service.ValidateBillingPricingMeter(ctx, "doubao-seedance", "token"))
	_, bill = prepareSeedanceCompletionBilling(context.Background(), h, key, subject, result.ResponseID, result)
	require.Nil(t, bill)
	require.Len(t, bindings.billed, 1, "repaired task is still claimed exactly once")
}

type legacyTaskUsageRepo struct {
	service.UsageLogRepository
	last *service.UsageLog
}

func (r *legacyTaskUsageRepo) Create(_ context.Context, log *service.UsageLog) (bool, error) {
	r.last = log
	return true, nil
}

type legacyTaskBillingRepo struct {
	service.UsageBillingRepository
	last *service.UsageBillingCommand
}

func (r *legacyTaskBillingRepo) Apply(_ context.Context, command *service.UsageBillingCommand) (*service.UsageBillingApplyResult, error) {
	r.last = command
	return &service.UsageBillingApplyResult{Applied: true}, nil
}

func TestSeedanceLegacyPendingDoesNotApplyNewPollPolicy(t *testing.T) {
	bindings := &grokMediaSlotBindings{}
	logs := &legacyTaskUsageRepo{}
	ledger := &legacyTaskBillingRepo{}
	cfg := &config.Config{}
	cfg.Default.RateMultiplier = 1
	gateway := service.NewOpenAIGatewayService(nil, logs, ledger, nil, nil, nil, bindings, cfg, nil, nil, service.NewBillingService(cfg, nil), nil, nil, nil, &service.DeferredService{}, nil, nil, nil, nil, nil, nil, nil, nil)
	h := &OpenAIGatewayHandler{gatewayService: gateway}
	groupID := int64(24)
	user := &service.User{ID: 10}
	key := &service.APIKey{ID: 20, UserID: 10, User: user, GroupID: &groupID, Group: &service.Group{ID: groupID, Platform: service.PlatformOpenAI, RateMultiplier: 1, Hydrated: true,
		ModelPricing: seedanceTokenPriceCard(2e-6), BillingPolicyVersion: 7, BillingRatePolicy: domain.BillingRatePolicy{PlatformMultipliers: map[string]float64{service.PlatformOpenAI: 2}}}}
	// Empty snapshot is a genuine task created before policy snapshots existed.
	pending := service.GrokVideoPendingBilling{Model: "doubao-seedance", BillingModel: "doubao-seedance", UpstreamModel: "doubao-seedance", OriginalModel: "legacy-public"}
	require.NoError(t, gateway.StoreGrokVideoPendingBilling(context.Background(), "seedance:legacy", user.ID, key.ID, pending))
	poll := gateway.WithBillingPricingSnapshot(context.Background(), key, time.Now(), "poll-public")
	result := &service.OpenAIForwardResult{ResponseID: "seedance:legacy", Usage: service.OpenAIUsage{OutputTokens: 12345}}
	ctx, bill := prepareSeedanceCompletionBilling(poll, h, key, middleware.AuthSubject{UserID: user.ID}, result.ResponseID, result)
	require.NotNil(t, bill)
	require.NoError(t, gateway.RecordUsage(ctx, &service.OpenAIRecordUsageInput{Result: bill, APIKey: key, User: user, Account: &service.Account{ID: 3, Platform: service.PlatformOpenAI, Type: service.AccountTypeAPIKey}, OriginalModel: "legacy-public"}))
	require.NotNil(t, ledger.last)
	require.InDelta(t, 0.02469, ledger.last.BalanceCost, 1e-12, "12345 output tokens × $2/M, without the new policy2")
	require.NotNil(t, logs.last)
	require.InDelta(t, 0.02469, logs.last.ActualCost, 1e-12)
	require.Zero(t, logs.last.BillingPolicyVersion)
	require.Equal(t, 1.0, logs.last.BillingRateSnapshot["policy_multiplier"])
	require.Equal(t, "legacy_current_prices", logs.last.BillingRateSnapshot["pricing_state"])
	require.Equal(t, int64(7), key.Group.BillingPolicyVersion)
	require.Equal(t, 2.0, key.Group.BillingRatePolicy.PlatformMultipliers[service.PlatformOpenAI], "poll snapshot/key remain immutable")
}

// Requirement: native Seedance task admission applies the OpenAI supplier rate
// before reserving slots or issuing a task, including when its native model has
// no Composite route. The separately persisted task cannot repair an already
// underpriced or invalid admission after the upstream has accepted it.
func TestCompositeSeedanceAdmissionPinsSupplierBeforePricing(t *testing.T) {
	for _, overflow := range []bool{false, true} {
		name := "accepted"
		factor := 3.0
		if overflow {
			name = "rate_overflow"
			factor = 600000
		}
		t.Run(name, func(t *testing.T) {
			h, slots, bindings, upstream := newGrokMediaSlotHandler(t, false, false, service.PlatformOpenAI)
			engine := &pricingAdmissionEngine{kind: securityaudit.DecisionAllow}
			h.securityAuditCoordinator = securityaudit.NewCoordinator(nil, engine)
			upstream.call = func(*http.Request, int64) (*http.Response, error) {
				return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(`{"id":"task-native","status":"queued"}`))}, nil
			}
			c, response := grokMediaSlotContext(context.Background(), true)
			c.Request = httptest.NewRequest(http.MethodPost, "/api/v3/contents/generations/tasks", strings.NewReader(`{"model":"doubao-seedance","content":[{"type":"text","text":"waves"}]}`))
			c.Request.Header.Set("Content-Type", "application/json")
			key, _ := middleware.GetAPIKeyFromContext(c)
			key.Group.Platform = service.PlatformComposite
			key.Group.RateMultiplier = 2
			key.Group.ModelPricing = seedanceTokenPriceCard(2e-6)
			key.Group.BillingRatePolicy = domain.BillingRatePolicy{PlatformMultipliers: map[string]float64{service.PlatformOpenAI: factor}}
			h.SeedanceTasks(c)
			require.Equal(t, int64(1), engine.calls.Load(), "valid native ingress must complete its content audit")
			require.False(t, engine.observedSnapshot.Load(), "pricing capture remains after audit")
			actual, ok := service.ResolvedTargetPlatformFromContext(c.Request.Context())
			require.True(t, ok)
			require.Equal(t, service.PlatformOpenAI, actual)
			if overflow {
				require.Equal(t, http.StatusServiceUnavailable, response.Code, response.Body.String())
				require.Contains(t, response.Body.String(), "BILLING_RATE_UNAVAILABLE")
				require.Zero(t, slots.userAcquired, "invalid supplier rate cannot acquire user capacity")
				require.Zero(t, slots.acquired, "invalid supplier rate cannot select/acquire an account")
				require.Zero(t, upstream.calls, "invalid supplier rate cannot create a billable upstream task")
				require.Empty(t, bindings.pending)
				return
			}
			require.Equal(t, http.StatusOK, response.Code, response.Body.String())
			require.Equal(t, 1, upstream.calls)
			require.Len(t, bindings.pending, 1)
			snapshot, err := service.ExportBillingPricingSnapshot(c.Request.Context(), "doubao-seedance")
			require.NoError(t, err)
			var captured struct {
				Platform    string `json:"platform"`
				PublicModel string `json:"public_model"`
			}
			require.NoError(t, json.Unmarshal(snapshot, &captured))
			require.Equal(t, service.PlatformOpenAI, captured.Platform)
			require.Equal(t, "doubao-seedance", captured.PublicModel)
			slots.assertReleased(t)
		})
	}
}
