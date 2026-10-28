//go:build unit

package handler

import (
	"context"
	"errors"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/config"
	"github.com/LuckyKuang/sub2api-plus/internal/domain"
	"github.com/LuckyKuang/sub2api-plus/internal/pkg/ctxkey"
	"github.com/LuckyKuang/sub2api-plus/internal/pkg/tlsfingerprint"
	middleware "github.com/LuckyKuang/sub2api-plus/internal/server/middleware"
	"github.com/LuckyKuang/sub2api-plus/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

const (
	promptPricingPrimaryGroup  = int64(9540)
	promptPricingFallbackGroup = int64(9541)
	promptPricingPrimaryOwner  = int64(9550)
	promptPricingFallbackOwner = int64(9551)
	promptPricingModel         = "claude-opus-4-6"
)

type promptFallbackRateRepo struct {
	service.UserGroupRateRepository
	failedGroup int64
	groups      []int64
}

func (r *promptFallbackRateRepo) GetByUserAndGroup(_ context.Context, _, groupID int64) (*float64, error) {
	r.groups = append(r.groups, groupID)
	if groupID == r.failedGroup {
		return nil, errors.New("personal rate storage unavailable")
	}
	rate := 0.5
	if groupID == promptPricingFallbackGroup {
		rate = 0.75
	}
	return &rate, nil
}

type promptFallbackWireCall struct {
	owner          int64
	group          int64
	platform       string
	publicModel    string
	upstreamModel  string
	forcedPlatform string
	pricingAt      time.Time
	reservation    float64
	authorization  string
}

type promptFallbackPricingWire struct {
	service.HTTPUpstream
	primarySuccess bool
	calls          []promptFallbackWireCall
}

func (u *promptFallbackPricingWire) Do(req *http.Request, _ string, accountID int64, _ int) (*http.Response, error) {
	call := promptFallbackWireCall{owner: accountID, authorization: req.Header.Get("x-api-key")}
	if group, _ := req.Context().Value(ctxkey.Group).(*service.Group); group != nil {
		call.group = group.ID
	}
	call.platform, _ = service.ResolvedTargetPlatformFromContext(req.Context())
	call.publicModel, _ = service.RequestedPublicModelFromContext(req.Context())
	call.upstreamModel, _ = service.ResolvedUpstreamModelFromContext(req.Context())
	call.forcedPlatform, _ = req.Context().Value(ctxkey.ForcePlatform).(string)
	if snapshot := service.BillingPricingSnapshotFromContext(req.Context()); snapshot != nil {
		call.pricingAt = snapshot.PricingAt()
	}
	call.reservation = service.InflightReservationFromContext(req.Context()).Amount()
	u.calls = append(u.calls, call)
	if accountID == promptPricingPrimaryOwner {
		if !u.primarySuccess {
			return &http.Response{StatusCode: http.StatusBadRequest, Header: http.Header{"Content-Type": {"application/json"}},
				Body: io.NopCloser(strings.NewReader(`{"error":{"message":"Prompt is too long"}}`))}, nil
		}
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"text/event-stream"}, "X-Request-Id": {"primary-priced"}},
			Body: io.NopCloser(strings.NewReader(`data: {"response":{"candidates":[{"content":{"parts":[{"text":"ok"}]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":1000,"candidatesTokenCount":2000}}}` + "\n\n"))}, nil
	}
	if accountID != promptPricingFallbackOwner {
		return nil, errors.New("unexpected credential owner")
	}
	return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"application/json"}, "X-Request-Id": {"fallback-priced"}},
		Body: io.NopCloser(strings.NewReader(`{"id":"msg_fallback","type":"message","role":"assistant","model":"claude-opus-4-6","content":[{"type":"text","text":"ok"}],"stop_reason":"end_turn","usage":{"input_tokens":1000,"output_tokens":2000}}`))}, nil
}

func (u *promptFallbackPricingWire) DoWithTLS(req *http.Request, proxy string, accountID int64, concurrency int, _ *tlsfingerprint.Profile) (*http.Response, error) {
	return u.Do(req, proxy, accountID, concurrency)
}

type promptFallbackUsageRepo struct {
	service.UsageLogRepository
	logs []*service.UsageLog
}

func (r *promptFallbackUsageRepo) Create(_ context.Context, log *service.UsageLog) (bool, error) {
	r.logs = append(r.logs, log)
	return true, nil
}

type promptFallbackPricingFixture struct {
	handler  *GatewayHandler
	key      *service.APIKey
	wire     *promptFallbackPricingWire
	rates    *promptFallbackRateRepo
	usage    *promptFallbackUsageRepo
	ledger   *compatStreamUsageBillingRepo
	cache    *handlerInflightCache
	schedule *groupScopedSchedulerCache
	oldAt    time.Time
}

func newPromptFallbackPricingFixture(t *testing.T, failure string) promptFallbackPricingFixture {
	t.Helper()
	gin.SetMode(gin.TestMode)
	cfg := &config.Config{Gateway: config.GatewayConfig{MaxLineSize: 1 << 20}}
	cfg.Default.RateMultiplier = 1
	cfg.Billing.InflightReservation = config.InflightReservationConfig{Enabled: true, TTLSeconds: 60}
	fallbackID := promptPricingFallbackGroup
	primaryInput, primaryOutput := 9e-6, 9e-6
	primary := &service.Group{ID: promptPricingPrimaryGroup, Hydrated: true, Status: service.StatusActive, Platform: service.PlatformComposite, RateMultiplier: 9,
		FallbackGroupIDOnInvalidRequest: &fallbackID, BillingPolicyVersion: 3,
		ModelPricing:      []service.ChannelModelPricing{{Platform: service.PlatformAntigravity, Models: []string{promptPricingModel}, InputPrice: &primaryInput, OutputPrice: &primaryOutput}},
		BillingRatePolicy: domain.BillingRatePolicy{PlatformMultipliers: map[string]float64{service.PlatformAntigravity: 2}}}
	fallbackInput, fallbackOutput := 1e-6, 2e-6
	fallback := &service.Group{ID: fallbackID, Hydrated: true, Status: service.StatusActive, Platform: service.PlatformAnthropic, RateMultiplier: 9,
		BillingPolicyVersion: 8, ModelPricing: []service.ChannelModelPricing{{Platform: service.PlatformAnthropic, Models: []string{promptPricingModel}, InputPrice: &fallbackInput, OutputPrice: &fallbackOutput}},
		BillingRatePolicy: domain.BillingRatePolicy{PlatformMultipliers: map[string]float64{service.PlatformAnthropic: 10},
			ModelMultipliers: []domain.BillingModelRate{{Platform: service.PlatformAnthropic, Model: promptPricingModel, Multiplier: 4}, {Platform: service.PlatformAnthropic, Model: "primary-public", Multiplier: 99}}}}
	if failure == "invalid_policy" {
		fallback.BillingRatePolicy.PlatformMultipliers[service.PlatformAnthropic] = math.Inf(1)
	}
	rates := &promptFallbackRateRepo{}
	if failure == "personal_rate_unavailable" {
		rates.failedGroup = fallbackID
	}
	accountRate := 0.5
	accounts := []*service.Account{
		{ID: promptPricingPrimaryOwner, Platform: service.PlatformAntigravity, Type: service.AccountTypeOAuth, Status: service.StatusActive, Schedulable: true, Concurrency: 1,
			Credentials:   map[string]any{"access_token": "primary-token", "project_id": "test-project", "expires_at": time.Now().Add(time.Hour).Format(time.RFC3339), "model_mapping": map[string]any{promptPricingModel: promptPricingModel}},
			AccountGroups: []service.AccountGroup{{AccountID: promptPricingPrimaryOwner, GroupID: promptPricingPrimaryGroup}}},
		{ID: promptPricingFallbackOwner, Platform: service.PlatformAnthropic, Type: service.AccountTypeAPIKey, Status: service.StatusActive, Schedulable: true, Concurrency: 1,
			RateMultiplier: &accountRate, Credentials: map[string]any{"api_key": "fallback-token"},
			AccountGroups: []service.AccountGroup{{AccountID: promptPricingFallbackOwner, GroupID: promptPricingFallbackGroup}}},
	}
	schedule := &groupScopedSchedulerCache{fakeSchedulerCache: &fakeSchedulerCache{accounts: accounts}}
	snapshot := service.NewSchedulerSnapshotService(schedule, nil, nil, nil, nil)
	groups := &groupMapRepo{fakeGroupRepo: &fakeGroupRepo{}, groups: map[int64]*service.Group{primary.ID: primary, fallback.ID: fallback}}
	wire := &promptFallbackPricingWire{primarySuccess: failure == "primary_success"}
	usage, ledger := &promptFallbackUsageRepo{}, &compatStreamUsageBillingRepo{}
	cache := newHandlerInflightCache(100)
	billingCache := service.NewBillingCacheService(cache, nil, nil, nil, nil, nil, cfg, nil)
	t.Cleanup(billingCache.Stop)
	billing := service.NewBillingService(cfg, nil)
	concurrency := service.NewConcurrencyService(&fakeConcurrencyCache{})
	gateway := service.NewGatewayService(nil, groups, usage, ledger, nil, nil, rates, nil, cfg, snapshot, concurrency, billing, nil, nil, nil, wire,
		&service.DeferredService{}, nil, nil, nil, nil, nil, nil, nil, service.NewModelPricingResolver(nil, billing), nil, nil, nil)
	settings := service.NewSettingService(&contentModerationHandlerSettingRepo{values: map[string]string{}}, cfg)
	antigravity := service.NewAntigravityGatewayService(nil, nil, snapshot, &service.AntigravityTokenProvider{}, nil, wire, settings, nil)
	key := &service.APIKey{ID: 9560, UserID: 9570, GroupID: &primary.ID, Group: primary,
		User: &service.User{ID: 9570, Balance: 100, Concurrency: 1}}
	return promptFallbackPricingFixture{handler: &GatewayHandler{gatewayService: gateway, antigravityGatewayService: antigravity,
		billingCacheService: billingCache, concurrencyHelper: NewConcurrencyHelper(concurrency, SSEPingFormatClaude, 0), cfg: cfg, maxAccountSwitches: 1},
		key: key, wire: wire, rates: rates, usage: usage, ledger: ledger, cache: cache, schedule: schedule,
		oldAt: time.Date(2026, 1, 1, 1, 0, 0, 0, time.UTC)}
}

func (f promptFallbackPricingFixture) send() *httptest.ResponseRecorder {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	ctx := context.WithValue(context.Background(), ctxkey.Group, f.key.Group)
	ctx = context.WithValue(ctx, ctxkey.ForcePlatform, service.PlatformAntigravity)
	ctx = service.WithCompositeRouteDecision(ctx, service.CompositeRouteDecision{Matched: true, TargetPlatform: service.PlatformAntigravity,
		PublicModel: "primary-public", UpstreamModel: promptPricingModel, Source: service.CompositeRouteSourceExplicit})
	ctx = f.handler.gatewayService.WithBillingPricingSnapshot(ctx, f.key, f.oldAt, "primary-public")
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(`{"model":"claude-opus-4-6","max_tokens":64,"messages":[{"role":"user","content":"hello"}]}`)).WithContext(ctx)
	c.Request.Header.Set("Content-Type", "application/json")
	c.Set(string(middleware.ContextKeyAPIKey), f.key)
	c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: f.key.UserID, Concurrency: 1})
	f.handler.Messages(c)
	return recorder
}

func TestGatewayPromptTooLongFallbackUsesNewGroupPricingAndSupplier(t *testing.T) {
	f := newPromptFallbackPricingFixture(t, "")
	started := time.Now()
	recorder := f.send()
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	require.Len(t, f.wire.calls, 2)
	primary, fallback := f.wire.calls[0], f.wire.calls[1]
	require.Equal(t, promptPricingPrimaryOwner, primary.owner)
	require.Equal(t, f.oldAt, primary.pricingAt, "the original admitted snapshot is not overwritten by a later pricing context")
	require.Equal(t, promptPricingFallbackOwner, fallback.owner)
	require.Equal(t, promptPricingFallbackGroup, fallback.group)
	require.Equal(t, service.PlatformAnthropic, fallback.platform)
	require.Empty(t, fallback.forcedPlatform)
	require.Empty(t, fallback.publicModel, "the primary Composite public alias must not select the fallback policy")
	require.Empty(t, fallback.upstreamModel)
	require.Equal(t, "fallback-token", fallback.authorization)
	require.False(t, fallback.pricingAt.Before(started), "a direct fallback admission uses current prices")
	require.Greater(t, fallback.reservation, 0.0)
	require.NotEqual(t, primary.reservation, fallback.reservation, "fallback admission replaces the original balance reservation")
	require.Equal(t, []int64{promptPricingPrimaryGroup, promptPricingFallbackGroup}, f.rates.groups)
	require.Len(t, f.ledger.commands, 1)
	require.InDelta(t, 0.15, f.ledger.commands[0].BalanceCost, 1e-12, "(1000 × $1/M + 2000 × $2/M) × personal0.75 × platform10 × model4")
	require.Len(t, f.usage.logs, 1)
	log := f.usage.logs[0]
	require.NotNil(t, log.GroupID)
	require.Equal(t, promptPricingFallbackGroup, *log.GroupID)
	require.Equal(t, promptPricingFallbackOwner, log.AccountID)
	require.EqualValues(t, 8, log.BillingPolicyVersion)
	require.Equal(t, service.PlatformAnthropic, log.BillingRateSnapshot["platform"])
	require.Equal(t, promptPricingModel, log.BillingRateSnapshot["model"])
	require.Equal(t, "personal", log.BillingRateSnapshot["base_source"])
	require.Equal(t, 0.75, log.BillingRateSnapshot["base_multiplier"])
	require.Equal(t, 40.0, log.BillingRateSnapshot["policy_multiplier"])
	require.Equal(t, 30.0, log.RateMultiplier)
	require.InDelta(t, 0.005, log.TotalCost, 1e-12)
	require.InDelta(t, 0.15, log.ActualCost, 1e-12)
	require.Equal(t, promptPricingPrimaryGroup, f.key.Group.ID, "the authenticated key and original group remain immutable")
	require.Zero(t, f.cache.count())
}

func TestGatewayPromptTooLongFallbackUnavailablePricesStopBeforeFallbackSideEffects(t *testing.T) {
	for _, failure := range []string{"invalid_policy", "personal_rate_unavailable"} {
		t.Run(failure, func(t *testing.T) {
			f := newPromptFallbackPricingFixture(t, failure)
			recorder := f.send()
			require.Equal(t, http.StatusServiceUnavailable, recorder.Code, recorder.Body.String())
			require.Contains(t, recorder.Body.String(), "BILLING_RATE_UNAVAILABLE")
			require.Len(t, f.wire.calls, 1, "invalid fallback rates cannot send another provider request")
			require.NotContains(t, f.schedule.queriedGroupIDs(), promptPricingFallbackGroup, "invalid rates precede fallback account selection")
			require.Empty(t, f.ledger.commands)
			require.Empty(t, f.usage.logs)
			require.Zero(t, f.cache.count(), "the original reservation is still released on the failure path")
		})
	}
}

func TestGatewayPromptTooLongPricingKeepsSuccessfulPrimaryGroupEvidence(t *testing.T) {
	f := newPromptFallbackPricingFixture(t, "primary_success")
	recorder := f.send()
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	require.Len(t, f.wire.calls, 1)
	require.Equal(t, f.oldAt, f.wire.calls[0].pricingAt)
	require.Equal(t, []int64{promptPricingPrimaryGroup}, f.rates.groups)
	require.Len(t, f.ledger.commands, 1)
	require.InDelta(t, 0.027, f.ledger.commands[0].BalanceCost, 1e-12, "3000 tokens × $9/M × personal0.5 × platform2")
	require.Len(t, f.usage.logs, 1)
	log := f.usage.logs[0]
	require.NotNil(t, log.GroupID)
	require.Equal(t, promptPricingPrimaryGroup, *log.GroupID)
	require.Equal(t, promptPricingPrimaryOwner, log.AccountID)
	require.EqualValues(t, 3, log.BillingPolicyVersion)
	require.Equal(t, service.PlatformAntigravity, log.BillingRateSnapshot["platform"])
	require.Equal(t, "primary-public", log.BillingRateSnapshot["model"])
	require.Equal(t, f.oldAt.Format(time.RFC3339Nano), log.BillingRateSnapshot["as_of"])
	require.InDelta(t, 0.027, log.ActualCost, 1e-12)
	require.Zero(t, f.cache.count())
}
