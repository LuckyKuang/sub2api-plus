//go:build unit

package service

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/config"
	"github.com/LuckyKuang/sub2api-plus/internal/domain"
	"github.com/stretchr/testify/require"
)

func TestBatchImagePolicySnapshotSurvivesHoldRecoveryAndSettlement(t *testing.T) {
	svc, repo, _, _, _ := newTestBatchImagePublicService(true)
	groupID := int64(7)
	group := &Group{ID: groupID, Platform: PlatformGemini, RateMultiplier: 2,
		AllowImageGeneration: true, AllowBatchImageGeneration: true,
		ImageRateIndependent: true, ImageRateMultiplier: 1.25,
		BatchImageDiscountMultiplier: 0.5, BatchImageHoldMultiplier: 0.6,
		BillingPolicyVersion: 12,
		BillingRatePolicy: domain.BillingRatePolicy{
			PlatformMultipliers: map[string]float64{PlatformGemini: 3},
			ModelMultipliers:    []domain.BillingModelRate{{Platform: PlatformGemini, Model: "gemini-2.5-flash-image", Multiplier: 1.5, BillingUnit: "image"}},
		}}
	svc.GroupRepo = &publicBatchImageGroupRepo{groups: map[int64]*Group{groupID: group}}
	personal := 0.4
	svc.UserGroupRateRepo = &publicBatchImageUserGroupRateRepo{rates: map[int64]*float64{groupID: &personal}}
	got, err := svc.Submit(context.Background(), BatchImageOwner{UserID: 11, APIKeyID: 22, GroupID: &groupID}, validBatchImageSubmitRequest(), "policy-task")
	require.NoError(t, err)
	job := repo.jobs[got.ID]
	require.InDelta(t, 5.625, job.GroupRateMultiplier, 1e-12)
	require.InDelta(t, 1.40625, got.EstimatedCost, 1e-12)
	billing := svc.BillingRepo.(*fakeBatchImageBillingRepo)
	require.Len(t, billing.reserves, 1)
	require.InDelta(t, 1.6875, billing.reserves[0].HoldAmount, 1e-12)

	// Another process recovers only the durable job after all live prices change.
	data, err := json.Marshal(job)
	require.NoError(t, err)
	var recovered BatchImageJob
	require.NoError(t, json.Unmarshal(data, &recovered))
	group.ImageRateMultiplier = 99
	group.BillingPolicyVersion = 13
	group.BillingRatePolicy.ModelMultipliers[0].Multiplier = 7
	personal = 8
	recovered.Status = BatchImageJobStatusSettling
	recovered.SuccessCount, recovered.FailCount = 1, 1
	repo.jobs[got.ID] = &recovered
	require.NoError(t, reserveBatchImageBalanceHold(context.Background(), billing, &recovered, "recovered"))
	require.InDelta(t, 1.6875, billing.reserves[1].HoldAmount, 1e-12)
	settlement := &BatchImageSettlementService{Repo: repo, BillingRepo: billing, Pricing: &fakeBatchImagePricingResolver{unitPrice: 100}}
	result, err := settlement.Settle(context.Background(), got.ID)
	require.NoError(t, err)
	require.InDelta(t, 0.703125, result.ActualCost, 1e-12)
	require.Len(t, billing.captures, 1)
	usage := billing.captures[0].UsageLog
	require.EqualValues(t, 12, usage.BillingPolicyVersion)
	require.Equal(t, PlatformGemini, usage.BillingRateSnapshot["platform"])
	require.Equal(t, "gemini-2.5-flash-image", usage.BillingRateSnapshot["model"])
	require.Equal(t, "image", usage.BillingRateSnapshot["billing_unit"])
	require.Equal(t, "media", usage.BillingRateSnapshot["base_source"])
	require.InDelta(t, 4.5, usage.BillingRateSnapshot["policy_multiplier"], 1e-12)
	require.InDelta(t, 2.8125, usage.RateMultiplier, 1e-12)
	require.NotContains(t, string(data), "test-secret")

	again, err := settlement.Settle(context.Background(), got.ID)
	require.NoError(t, err)
	require.True(t, again.AlreadySettled)
	require.Len(t, billing.captures, 1)
}

func TestBatchImageSubmitUsesAdmissionPricingSnapshot(t *testing.T) {
	svc, repo, _, _, _ := newTestBatchImagePublicService(true)
	groupID := int64(7)
	group := &Group{ID: groupID, Platform: PlatformGemini, RateMultiplier: 2,
		AllowBatchImageGeneration: true, BatchImageDiscountMultiplier: 0.5, BatchImageHoldMultiplier: 0.6,
		BillingPolicyVersion: 5, BillingRatePolicy: domain.BillingRatePolicy{PlatformMultipliers: map[string]float64{PlatformGemini: 3}}}
	ctx := withBillingPricingSnapshot(context.Background(), &APIKey{Group: group}, time.Date(2026, 1, 1, 1, 0, 0, 0, time.UTC), nil, nil, nil, "gemini-2.5-flash-image")
	// The post-admission database generation must not change this task's charges.
	group.RateMultiplier = 9
	group.BillingPolicyVersion = 6
	group.BillingRatePolicy.PlatformMultipliers[PlatformGemini] = 9
	svc.GroupRepo = &publicBatchImageGroupRepo{groups: map[int64]*Group{groupID: group}}
	got, err := svc.Submit(ctx, BatchImageOwner{UserID: 11, APIKeyID: 22, GroupID: &groupID}, validBatchImageSubmitRequest(), "")
	require.NoError(t, err)
	require.InDelta(t, 6, repo.jobs[got.ID].GroupRateMultiplier, 1e-12)
	require.InDelta(t, 1.5, got.EstimatedCost, 1e-12)
}

func TestLiveFinalizationRetainsAdmittedPolicyWithoutInventingCharges(t *testing.T) {
	group := &Group{ID: 7, Platform: PlatformOpenAI, RateMultiplier: 2, BillingPolicyVersion: 12,
		BillingRatePolicy: domain.BillingRatePolicy{PlatformMultipliers: map[string]float64{PlatformOpenAI: 3}}}
	at := time.Date(2026, 1, 1, 1, 0, 0, 0, time.UTC)
	ctx := withBillingPricingSnapshot(context.Background(), &APIKey{Group: group}, at, nil, nil, nil, "gpt-live")
	snapshot, err := ExportBillingPricingSnapshot(ctx, "gpt-live")
	require.NoError(t, err)
	require.NotEmpty(t, snapshot)
	record := &LiveCallRecord{CallID: "call-policy", CallHash: hashLiveCallID("call-policy"),
		AccountID: 11, APIKeyID: 22, UserID: 33, GroupID: 7, LeaseID: "lease-policy", Model: "gpt-live",
		CreatedAt: time.Now().Add(-time.Second), ExpiresAt: time.Now().Add(time.Hour),
		Controller: LiveControllerPending, PricingSnapshot: snapshot}
	data, err := json.Marshal(record)
	require.NoError(t, err)
	var recovered LiveCallRecord
	require.NoError(t, json.Unmarshal(data, &recovered))
	group.BillingPolicyVersion = 13
	group.RateMultiplier = 99
	store, usageRepo := &liveTestStore{}, &liveTestUsageRepo{}
	require.NoError(t, store.SaveLiveCall(context.Background(), &recovered, time.Hour))
	svc := &OpenAIGatewayService{cache: store, usageLogRepo: usageRepo,
		concurrencyService: NewConcurrencyService(&liveTestConcurrencyCache{})}
	svc.finalizeLiveCall(&recovered)
	svc.finalizeLiveCall(&recovered)
	usageRepo.mu.Lock()
	defer usageRepo.mu.Unlock()
	require.Len(t, usageRepo.logs, 1)
	log := usageRepo.logs[0]
	require.EqualValues(t, 12, log.BillingPolicyVersion)
	require.Equal(t, "non_billable_live", log.BillingRateSnapshot["pricing_state"])
	require.Equal(t, at.Format(time.RFC3339Nano), log.BillingRateSnapshot["as_of"])
	require.Zero(t, log.TotalCost)
	require.Zero(t, log.ActualCost)
	require.Equal(t, float64(1), log.RateMultiplier)
}

type durableVideoPricingCache struct {
	GatewayCache
	mu   sync.Mutex
	data map[string][]byte
}

func (c *durableVideoPricingCache) SetGrokVideoPendingBilling(_ context.Context, key string, data []byte, _ time.Duration) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.data[key] = append([]byte(nil), data...)
	return nil
}

func (c *durableVideoPricingCache) GetGrokVideoPendingBilling(_ context.Context, key string) ([]byte, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]byte(nil), c.data[key]...), nil
}

func TestGrokVideoPendingBillingRetainsCreatePolicyAcrossLaterRequests(t *testing.T) {
	videoPrice := 0.07
	group := &Group{ID: 7, Platform: PlatformComposite, RateMultiplier: 2, BillingPolicyVersion: 12,
		VideoPrice720P: &videoPrice,
		BillingRatePolicy: domain.BillingRatePolicy{ModelMultipliers: []domain.BillingModelRate{
			{Platform: PlatformGrok, Model: "public-video", BillingUnit: "second", Multiplier: 0.25},
		}}}
	at := time.Date(2026, 1, 1, 1, 0, 0, 0, time.UTC)
	ctx := withBillingPricingSnapshot(context.Background(), &APIKey{Group: group}, at, NewBillingService(&config.Config{}, nil), nil, nil, "public-video")
	cache := &durableVideoPricingCache{data: map[string][]byte{}}
	svc := &OpenAIGatewayService{cache: cache}
	require.NoError(t, svc.StoreGrokVideoPendingBilling(ctx, "video-task", 11, 22, GrokVideoPendingBilling{
		Model: "public-video", OriginalModel: "public-video", UpstreamModel: "grok-imagine-video", VideoDurationSeconds: 8,
	}))
	group.BillingPolicyVersion, group.RateMultiplier = 13, 99
	group.BillingRatePolicy.ModelMultipliers[0].Multiplier = 8
	videoPrice = 100
	pending, err := svc.LoadGrokVideoPendingBilling(context.Background(), "video-task", 11, 22)
	require.NoError(t, err)
	require.NotNil(t, pending)
	restored, err := RestoreBillingPricingSnapshot(context.Background(), pending.PricingSnapshot)
	require.NoError(t, err)
	snapshot := BillingPricingSnapshotFromContext(restored)
	require.NotNil(t, snapshot)
	quote := snapshot.quote(PlatformGrok, "public-video", "second")
	require.EqualValues(t, 12, quote.PolicyVersion)
	require.Equal(t, at, quote.AsOf)
	require.InDelta(t, 0.5, quote.EffectiveMultiplier, 1e-12)
	cost := snapshot.billing.CalculateVideoCost("grok-imagine-video", VideoBillingResolution720P, 1, 8, nil, quote.EffectiveMultiplier)
	require.InDelta(t, 0.56, cost.TotalCost, 1e-12)
	require.InDelta(t, 0.28, cost.ActualCost, 1e-12)
	require.NotContains(t, string(pending.PricingSnapshot), "credentials")
	missing, err := svc.LoadGrokVideoPendingBilling(context.Background(), "video-task", 11, 99)
	require.NoError(t, err)
	require.Nil(t, missing, "another API key cannot restore a different owner's task prices")
}

func TestAsyncMediaPricingSnapshotPreservesActualSupplier(t *testing.T) {
	group := &Group{ID: 7, Platform: PlatformComposite, RateMultiplier: 2, BillingPolicyVersion: 12,
		BillingRatePolicy: domain.BillingRatePolicy{PlatformMultipliers: map[string]float64{PlatformOpenAI: 0.25, PlatformGrok: 99}}}
	ctx := withBillingPricingSnapshot(WithResolvedTargetPlatform(context.Background(), PlatformOpenAI), &APIKey{Group: group}, time.Now(), nil, nil, nil, "public-seedance")
	svc := &OpenAIGatewayService{cache: &durableVideoPricingCache{data: map[string][]byte{}}}
	require.NoError(t, svc.StoreGrokVideoPendingBilling(ctx, "seedance:task", 11, 22, GrokVideoPendingBilling{Model: "public-seedance", OriginalModel: "public-seedance", UpstreamModel: "doubao-seedance"}))
	pending, err := svc.LoadGrokVideoPendingBilling(context.Background(), "seedance:task", 11, 22)
	require.NoError(t, err)
	require.NotNil(t, pending)
	restored, err := RestoreBillingPricingSnapshot(context.Background(), pending.PricingSnapshot)
	require.NoError(t, err)
	snapshot := BillingPricingSnapshotFromContext(restored)
	require.Equal(t, PlatformOpenAI, snapshot.platform)
	require.InDelta(t, 0.5, snapshot.quote(snapshot.platform, snapshot.publicModel, "token").EffectiveMultiplier, 1e-12)
}

func TestDurablePricingReexportPreservesAcceptedDeepSeekPeakFactor(t *testing.T) {
	// Accepted evidence is authoritative even when today's compiled schedule
	// would choose another factor for the recorded time.
	const model = "deepseek-v4.1-flash"
	stored := durableBillingPricingSnapshot{
		Version: 2, RateCalculation: BillingRateCalculationProduct, PeakScope: BillingPeakScopeAllMeters,
		Group: &Group{ID: 7, Platform: PlatformDeepseek, RateMultiplier: 1}, TokenPeak: billingFloat(1),
		PricingAt: time.Date(2026, 10, 19, 0, 0, 0, 0, time.UTC), Platform: PlatformDeepseek, PublicModel: model,
		Models:       map[string]*ModelPricing{model: {InputPricePerToken: 2e-6, OutputPricePerToken: 4e-6}},
		Images:       map[string]map[string]float64{model: {ImageBillingSize1K: .04, ImageBillingSize2K: .08, ImageBillingSize4K: .16}},
		Videos:       map[string]map[string]float64{model: {VideoBillingResolution480P: .05, VideoBillingResolution720P: .07, VideoBillingResolution1080P: .1}},
		CacheStorage: map[string]float64{model: 0}, DeepSeekPeakMultiplier: 2,
	}
	encoded, err := json.Marshal(stored)
	require.NoError(t, err)
	ctx, err := RestoreBillingPricingSnapshot(context.Background(), encoded)
	require.NoError(t, err)
	for range 2 {
		snapshot := BillingPricingSnapshotFromContext(ctx)
		cost, err := snapshot.billing.CalculateTokenCostForRequest(TokenCostRequest{
			Ctx: ctx, Model: model, Group: snapshot.group, Resolver: snapshot.resolver,
			Tokens: UsageTokens{InputTokens: 1000, OutputTokens: 1000}, RateMultiplier: 1,
		})
		require.NoError(t, err)
		require.InDelta(t, .012, cost.ActualCost, 1e-12, "($2 input + $4 output)/M × 1000 tokens × accepted peak 2")
		encoded, err = ExportBillingPricingSnapshot(ctx)
		require.NoError(t, err)
		var reexported durableBillingPricingSnapshot
		require.NoError(t, json.Unmarshal(encoded, &reexported))
		require.Equal(t, 2.0, reexported.DeepSeekPeakMultiplier)
		ctx, err = RestoreBillingPricingSnapshot(context.Background(), encoded)
		require.NoError(t, err)
	}
}
