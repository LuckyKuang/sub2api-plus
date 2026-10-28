//go:build unit

package handler

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/config"
	"github.com/LuckyKuang/sub2api-plus/internal/domain"
	"github.com/LuckyKuang/sub2api-plus/internal/repository"
	middleware "github.com/LuckyKuang/sub2api-plus/internal/server/middleware"
	"github.com/LuckyKuang/sub2api-plus/internal/service"
	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// This repository controls only the external billing failure. The production
// Redis repository supplies claim ownership, expiry, and cross-instance state.
type mediaRecoveryBillingRepo struct {
	service.UsageBillingRepository
	mu        sync.Mutex
	failNext  bool
	panicNext bool
	attempts  int
	charges   []*service.UsageBillingCommand
}

func (r *mediaRecoveryBillingRepo) Apply(ctx context.Context, command *service.UsageBillingCommand) (*service.UsageBillingApplyResult, error) {
	r.mu.Lock()
	r.attempts++
	fail := r.failNext
	r.failNext = false
	shouldPanic := r.panicNext
	r.panicNext = false
	r.mu.Unlock()
	if shouldPanic {
		panic("billing dependency panic before settlement")
	}
	if fail {
		// SQL billing owns a detached ten-second budget. Model its dependency
		// timing out after the caller's shorter worker budget has elapsed.
		timer := time.NewTimer(60 * time.Millisecond)
		defer timer.Stop()
		select {
		case <-timer.C:
			return nil, context.DeadlineExceeded
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	r.mu.Lock()
	copyCommand := *command
	r.charges = append(r.charges, &copyCommand)
	r.mu.Unlock()
	return &service.UsageBillingApplyResult{Applied: true, UsageLogPersisted: true}, nil
}

func (r *mediaRecoveryBillingRepo) snapshot() (int, []*service.UsageBillingCommand) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.attempts, append([]*service.UsageBillingCommand(nil), r.charges...)
}

type mediaRecoveryFixture struct {
	redis      *miniredis.Miniredis
	client     *redis.Client
	ledger     *mediaRecoveryBillingRepo
	handler    *OpenAIGatewayHandler
	key        *service.APIKey
	account    *service.Account
	model      string
	task       string
	platform   string
	completion *service.OpenAIForwardResult
}

func newMediaRecoveryFixture(t *testing.T, seedance bool) *mediaRecoveryFixture {
	t.Helper()
	f := &mediaRecoveryFixture{redis: miniredis.RunT(t), ledger: &mediaRecoveryBillingRepo{}, platform: service.PlatformGrok, model: "grok-imagine-video", task: "recovery-video"}
	if seedance {
		f.platform, f.model, f.task = service.PlatformOpenAI, "doubao-seedance", "seedance:recovery-video"
	}
	f.client = redis.NewClient(&redis.Options{Addr: f.redis.Addr()})
	t.Cleanup(func() { _ = f.client.Close() })
	price := 2.0
	group := &service.Group{ID: 24, Hydrated: true, Platform: service.PlatformComposite, RateMultiplier: 1,
		BillingPolicyVersion: 7, BillingRatePolicy: domain.BillingRatePolicy{
			PlatformMultipliers: map[string]float64{f.platform: 2},
			ModelMultipliers:    []domain.BillingModelRate{{Platform: f.platform, Model: f.model, Multiplier: 3}},
		}, ModelPricing: []service.ChannelModelPricing{{Platform: f.platform, Models: []string{f.model}, BillingMode: service.BillingModePerRequest, PerRequestPrice: &price}}}
	if seedance {
		group.ModelPricing = seedanceTokenPriceCard(2e-6)
	}
	f.key = &service.APIKey{ID: 20, UserID: 10, GroupID: &group.ID, Group: group, User: &service.User{ID: 10, Balance: 100}}
	f.account = &service.Account{ID: 30, Platform: f.platform, Type: service.AccountTypeAPIKey}
	f.handler = f.newHandler(t)
	ctx := f.handler.gatewayService.WithBillingPricingSnapshot(service.WithResolvedTargetPlatform(context.Background(), f.platform), f.key, time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC), f.model)
	require.NoError(t, service.ValidateBillingPricingSnapshot(ctx))
	require.NoError(t, f.handler.gatewayService.StoreGrokVideoPendingBilling(ctx, f.task, f.key.UserID, f.key.ID,
		service.GrokVideoPendingBilling{Model: f.model, BillingModel: f.model, UpstreamModel: f.model, OriginalModel: f.model, VideoDurationSeconds: 5, VideoResolution: "480p"}))
	f.completion = &service.OpenAIForwardResult{ResponseID: f.task, Model: f.model, BillingModel: f.model, VideoCount: 1, VideoDurationSeconds: 5, VideoResolution: "480p"}
	if seedance {
		f.completion.VideoCount = 0
		f.completion.Usage.OutputTokens = 1000000
	}
	return f
}

func (f *mediaRecoveryFixture) newHandler(t *testing.T) *OpenAIGatewayHandler {
	t.Helper()
	cfg := &config.Config{}
	cfg.Default.RateMultiplier = 1
	gateway := service.NewOpenAIGatewayService(nil, nil, f.ledger, nil, nil, nil, repository.NewGatewayCache(f.client), cfg, nil, nil,
		service.NewBillingService(cfg, nil), nil, nil, nil, &service.DeferredService{}, nil, nil, nil, nil, nil, nil, nil, nil)
	pool := service.NewUsageRecordWorkerPoolWithOptions(service.UsageRecordWorkerPoolOptions{WorkerCount: 1, QueueSize: 2, TaskTimeout: 30 * time.Millisecond})
	t.Cleanup(pool.Stop)
	return &OpenAIGatewayHandler{gatewayService: gateway, usageRecordWorkerPool: pool}
}

func (f *mediaRecoveryFixture) prepare(h *OpenAIGatewayHandler) (context.Context, *service.OpenAIForwardResult) {
	subject := middleware.AuthSubject{UserID: f.key.UserID}
	ctx := service.WithResolvedTargetPlatform(context.Background(), f.platform)
	if f.platform == service.PlatformOpenAI {
		return prepareSeedanceCompletionBilling(ctx, h, f.key, subject, f.task, f.completion)
	}
	return prepareGrokVideoCompletionBilling(ctx, h, zap.NewNop(), f.key, subject, f.task, f.completion)
}

func (f *mediaRecoveryFixture) record(t *testing.T, h *OpenAIGatewayHandler, ctx context.Context, bill *service.OpenAIForwardResult) {
	t.Helper()
	c, _ := grokMediaSlotContext(ctx, false)
	c.Set(string(middleware.ContextKeyAPIKey), f.key)
	recordGrokMediaUsage(c, h, zap.NewNop(), f.key, middleware.AuthSubject{UserID: f.key.UserID}, nil, f.account, bill, f.model, nil, f.task)
}

func waitMediaBillingTask(t *testing.T, h *OpenAIGatewayHandler) {
	t.Helper()
	require.Eventually(t, func() bool {
		stats := h.usageRecordWorkerPool.Stats()
		return stats.CompletedTasks == stats.SubmittedTasks && stats.RunningWorkers == 0 && stats.WaitingTasks == 0
	}, 2*time.Second, time.Millisecond)
}

// A transient billing timeout must free its claim even after its billing
// context expires. A later poll settles once at the accepted quote, despite a
// changed group price and both platform/model multipliers.
func TestMediaBillingTimeoutReleasesClaimAndRetriesOriginalQuote(t *testing.T) {
	for _, seedance := range []bool{false, true} {
		name := "grok"
		if seedance {
			name = "seedance"
		}
		t.Run(name, func(t *testing.T) {
			f := newMediaRecoveryFixture(t, seedance)
			f.ledger.failNext = true
			ctx, bill := f.prepare(f.handler)
			require.NotNil(t, bill)
			f.record(t, f.handler, ctx, bill)
			waitMediaBillingTask(t, f.handler)
			attempts, charges := f.ledger.snapshot()
			require.Equal(t, 1, attempts, "first completion must reach the failing billing dependency")
			require.Empty(t, charges)
			// Change today's prices after the accepted create. Recovery must use
			// $2 × platform 2 × model 3 = $12 from its persisted evidence.
			f.key.Group.RateMultiplier = 9
			f.key.Group.BillingRatePolicy.PlatformMultipliers[f.platform] = 7
			f.key.Group.BillingRatePolicy.ModelMultipliers[0].Multiplier = 8
			f.key.Group.ModelPricing = nil
			other := f.newHandler(t)
			ctx, bill = f.prepare(other)
			require.NotNil(t, bill, "an expired failed billing attempt must not block a recovered instance")
			f.record(t, other, ctx, bill)
			waitMediaBillingTask(t, other)
			attempts, charges = f.ledger.snapshot()
			require.Equal(t, 2, attempts)
			require.Len(t, charges, 1)
			require.InDelta(t, 12.0, charges[0].BalanceCost, 1e-12)
			require.Equal(t, service.StableGrokVideoBillingRequestID(f.task), charges[0].RequestID)
			_, bill = f.prepare(f.handler)
			require.Nil(t, bill, "successful settlement suppresses repeated status/content billing")
		})
	}
}

// A recovered worker panic must release the unsettled claim immediately. A
// subsequent instance must bill the accepted create quote without waiting for
// lease expiry or substituting prices changed after acceptance.
func TestMediaBillingPanicReleasesClaimAndRetriesOriginalQuote(t *testing.T) {
	for _, seedance := range []bool{false, true} {
		name := "grok"
		requestID := "grok-video:recovery-video"
		if seedance {
			name = "seedance"
			requestID = "grok-video:seedance:recovery-video"
		}
		t.Run(name, func(t *testing.T) {
			f := newMediaRecoveryFixture(t, seedance)
			f.ledger.panicNext = true
			if seedance {
				f.completion.ResponseID = "asset-first"
			}
			ctx, bill := f.prepare(f.handler)
			require.NotNil(t, bill)
			f.record(t, f.handler, ctx, bill)
			waitMediaBillingTask(t, f.handler)
			attempts, charges := f.ledger.snapshot()
			require.Equal(t, 1, attempts, "the injected panic must occur in the billing dependency")
			require.Empty(t, charges, "a pre-settlement panic cannot apply a charge")

			f.key.Group.RateMultiplier = 9
			f.key.Group.BillingRatePolicy.PlatformMultipliers[f.platform] = 7
			f.key.Group.BillingRatePolicy.ModelMultipliers[0].Multiplier = 8
			f.key.Group.ModelPricing = nil
			other := f.newHandler(t)
			if seedance {
				f.completion.ResponseID = "asset-later"
			}
			ctx, bill = f.prepare(other)
			require.NotNil(t, bill, "panic cleanup must permit immediate retry without advancing Redis time")
			f.record(t, other, ctx, bill)
			waitMediaBillingTask(t, other)
			attempts, charges = f.ledger.snapshot()
			require.Equal(t, 2, attempts)
			require.Len(t, charges, 1)
			require.InDelta(t, 12.0, charges[0].BalanceCost, 1e-12, "$2 × platform 2 × model 3 remains the accepted quote")
			require.Equal(t, requestID, charges[0].RequestID)
			_, bill = f.prepare(f.handler)
			require.Nil(t, bill, "a successful retry prevents repeated poll settlement")
			attempts, charges = f.ledger.snapshot()
			require.Equal(t, 2, attempts)
			require.Len(t, charges, 1)
		})
	}
}

// Simulate a process dying after Redis claim acquisition, before enqueue or
// charge. Its processing lease must expire while accepted pricing still exists,
// allowing another instance to settle without operator intervention.
func TestMediaAbandonedBillingClaimRecoversBeforePricingEvidenceExpires(t *testing.T) {
	for _, seedance := range []bool{false, true} {
		name := "grok"
		if seedance {
			name = "seedance"
		}
		t.Run(name, func(t *testing.T) {
			f := newMediaRecoveryFixture(t, seedance)
			_, bill := f.prepare(f.handler)
			require.NotNil(t, bill)
			other := f.newHandler(t)
			_, bill = f.prepare(other)
			require.Nil(t, bill, "another worker cannot bill while the first claim is live")
			// No callback is invoked: this is a lost worker, not a graceful
			// error path. A short recoverable lease is required within a minute.
			f.redis.FastForward(time.Minute)
			pending, err := other.gatewayService.LoadGrokVideoPendingBilling(context.Background(), f.task, f.key.UserID, f.key.ID)
			require.NoError(t, err)
			require.NotNil(t, pending, "recovery must precede expiry of the accepted price evidence")
			ctx, bill := f.prepare(other)
			require.NotNil(t, bill, "abandoned pre-commit claim must recover within one minute")
			f.record(t, other, ctx, bill)
			waitMediaBillingTask(t, other)
			attempts, charges := f.ledger.snapshot()
			require.Equal(t, 1, attempts)
			require.Len(t, charges, 1)
			require.InDelta(t, 12.0, charges[0].BalanceCost, 1e-12)
			_, bill = f.prepare(f.handler)
			require.Nil(t, bill)
		})
	}
}

// The previous generation wrote "1" before settlement. Deploying the fix must
// recover an unpaid task without deleting that ambiguous legacy cache record.
func TestMediaLegacyUnsettledBillingClaimRecoversAcceptedQuote(t *testing.T) {
	for _, seedance := range []bool{false, true} {
		name := "grok"
		if seedance {
			name = "seedance"
		}
		t.Run(name, func(t *testing.T) {
			f := newMediaRecoveryFixture(t, seedance)
			legacyKey := fmt.Sprintf("grok_video_billed:%d:%d:%s", f.key.UserID, f.key.ID, f.task)
			require.NoError(t, f.client.Set(context.Background(), legacyKey, "1", 48*time.Hour).Err())
			f.key.Group.RateMultiplier = 9
			f.key.Group.BillingRatePolicy.PlatformMultipliers[f.platform] = 7
			f.key.Group.BillingRatePolicy.ModelMultipliers[0].Multiplier = 8
			f.key.Group.ModelPricing = nil
			other := f.newHandler(t)
			ctx, bill := f.prepare(other)
			require.NotNil(t, bill, "an ambiguous old pre-commit claim cannot strand an unpaid task on upgrade")
			f.record(t, other, ctx, bill)
			waitMediaBillingTask(t, other)
			attempts, charges := f.ledger.snapshot()
			require.Equal(t, 1, attempts)
			require.Len(t, charges, 1)
			require.InDelta(t, 12.0, charges[0].BalanceCost, 1e-12)
			value, err := f.client.Get(context.Background(), legacyKey).Result()
			require.NoError(t, err)
			require.Equal(t, "1", value, "upgrade does not delete or rewrite unrelated legacy caches")
			_, bill = f.prepare(f.handler)
			require.Nil(t, bill)
		})
	}
}

// Status payload IDs can name generated assets, not the authenticated task.
// A recovered processing attempt must keep the route's durable billing key
// even when two valid provider observations return different nonempty IDs.
func TestGrokBillingTaskRouteControlsIdentityAcrossChangingAssetIDs(t *testing.T) {
	f := newMediaRecoveryFixture(t, false)
	f.completion.ResponseID = "asset-first-observation"
	ctx, first := f.prepare(f.handler)
	require.NotNil(t, first)
	require.Equal(t, "asset-first-observation", first.ResponseID, "provider observations remain available independently of billing identity")
	// RecordUsage commits, then the process is lost before publishing the
	// Redis completion marker. No cleanup/finalization callback runs here.
	require.NoError(t, f.handler.gatewayService.RecordUsage(ctx, &service.OpenAIRecordUsageInput{
		Result: first, APIKey: f.key, User: f.key.User, Account: f.account, OriginalModel: f.model,
		RequestPayloadHash: service.HashUsageRequestPayload([]byte(f.task)),
	}))
	f.redis.FastForward(time.Minute)
	f.completion.ResponseID = "asset-second-observation"
	other := f.newHandler(t)
	ctx, second := f.prepare(other)
	require.NotNil(t, second)
	require.Equal(t, "asset-second-observation", second.ResponseID)
	// Exercise the normal recording helper as well: it must not replace the
	// route-owned key with the later response's distinct asset identifier.
	f.record(t, other, ctx, second)
	waitMediaBillingTask(t, other)
	attempts, commands := f.ledger.snapshot()
	require.Equal(t, 2, attempts)
	require.Len(t, commands, 2, "both recovered attempts must reach durable SQL dedup")
	for _, command := range commands {
		require.Equal(t, "grok-video:recovery-video", command.RequestID,
			"provider asset IDs cannot change the owned task's durable billing identity")
		require.InDelta(t, 12.0, command.BalanceCost, 1e-12)
	}
	require.Equal(t, commands[0].RequestFingerprint, commands[1].RequestFingerprint,
		"recovered status/content attempts must remain the same durable transaction")
}
