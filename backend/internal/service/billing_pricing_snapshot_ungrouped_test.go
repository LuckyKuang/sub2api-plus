//go:build unit

package service

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/config"
	"github.com/stretchr/testify/require"
)

func TestUngroupedBillingSnapshot_PinsReservationRetrySettlementAndDurablePrices(t *testing.T) {
	for _, tc := range []struct {
		name          string
		changeSetting bool
		changeCatalog bool
		changeRate    bool
		nextCharge    float64
	}{
		{name: "fallback settings edit", changeSetting: true, nextCharge: 0.030},
		{name: "catalog reload", changeCatalog: true, nextCharge: 0.036},
		{name: "settings catalog and default rate edit", changeSetting: true, changeCatalog: true, changeRate: true, nextCharge: 0.045},
	} {
		t.Run(tc.name, func(t *testing.T) {
			const model = "ungrouped-unpublished-public-sku"
			catalog := newStubPricingServiceFromJSON(t, `{
				"fallback-a": {"input_cost_per_token":0.000002,"output_cost_per_token":0.000006},
				"fallback-b": {"input_cost_per_token":0.000005,"output_cost_per_token":0.000010}
			}`)
			logs := &openAIRecordUsageLogRepoStub{inserted: true}
			ledger := &openAIRecordUsageBillingRepoStub{result: &UsageBillingApplyResult{Applied: true, UsageLogPersisted: true}}
			svc := newOpenAIRecordUsageServiceWithBillingRepoForTest(logs, ledger,
				&openAIRecordUsageUserRepoStub{}, &openAIRecordUsageSubRepoStub{}, nil)
			svc.cfg.Default.RateMultiplier = 2
			svc.cfg.Billing.InflightReservation = config.InflightReservationConfig{
				Enabled: true, DefaultMaxTokens: 1000, MaxInputTokens: 1000, MaxOutputTokens: 1000,
			}
			billing := NewBillingService(svc.cfg, catalog)
			configured := map[string]string{PlatformOpenAI: "fallback-a"}
			reads := 0
			billing.pricingFallbackModels = func(context.Context) (map[string]string, error) {
				reads++
				return configured, nil
			}
			svc.billingService, svc.resolver = billing, NewModelPricingResolver(nil, billing)
			user := &User{ID: 22, Balance: 10}
			key := &APIKey{ID: 11, UserID: user.ID, User: user}
			account := &Account{ID: 33, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Status: StatusActive, Schedulable: true}
			at := time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)
			ctx := svc.WithBillingPricingSnapshot(context.Background(), key, at, model)
			snapshot := BillingPricingSnapshotFromContext(ctx)
			require.NotNil(t, snapshot)
			require.True(t, snapshot.ungrouped)
			require.Nil(t, snapshot.group)
			require.Nil(t, key.Group)
			require.Nil(t, key.GroupID)
			require.Equal(t, PlatformOpenAI, snapshot.platform)
			require.NoError(t, ValidateBillingPricingSnapshot(ctx))
			require.NoError(t, ValidateBillingPricingMeter(ctx, model, "token"))
			require.Equal(t, 1, reads)
			request := InflightEstimateRequest{Model: model, BodyBytes: 4000, MaxTokens: 1000, Kind: InflightEstimateToken}
			hold, priced := svc.EstimateInflightReservation(ctx, key, request)
			require.True(t, priced)
			// 1000 input at $2/M + 1000 output at $6/M, then the frozen 2x rate.
			require.InDelta(t, 0.016, hold, 1e-12)

			if tc.changeSetting {
				configured[PlatformOpenAI] = "fallback-b"
			}
			if tc.changeCatalog {
				catalog.mu.Lock()
				catalog.pricingData = map[string]*LiteLLMModelPricing{
					"fallback-a": {InputCostPerToken: 7e-6, OutputCostPerToken: 11e-6},
					"fallback-b": {InputCostPerToken: 5e-6, OutputCostPerToken: 10e-6},
				}
				catalog.mu.Unlock()
			}
			if tc.changeRate {
				svc.cfg.Default.RateMultiplier = 3
			}
			retry := svc.WithBillingPricingSnapshot(CopyBillingPricingSnapshot(ctx, context.Background()), key, at.Add(time.Hour), model)
			require.Same(t, snapshot, BillingPricingSnapshotFromContext(retry))
			hold, priced = svc.EstimateInflightReservation(retry, key, request)
			require.True(t, priced)
			require.InDelta(t, 0.016, hold, 1e-12)
			selected, selectedPriced := svc.EstimateInflightReservationForAccount(retry, key, request, account)
			require.True(t, selectedPriced)
			require.InDelta(t, 0.016, selected, 1e-12)
			require.Equal(t, 1, reads, "retries must not reread the platform fallback configuration")

			encoded, err := ExportBillingPricingSnapshot(retry, model)
			require.NoError(t, err)
			require.Contains(t, string(encoded), `"ungrouped":true`)
			require.Contains(t, string(encoded), `"default_rate":2`)
			durable, err := RestoreBillingPricingSnapshot(context.Background(), encoded)
			require.NoError(t, err)
			require.Nil(t, BillingPricingSnapshotFromContext(durable).group)
			require.NoError(t, ValidateBillingPricingMeter(durable, model, "token"))
			hold, priced = svc.EstimateInflightReservation(durable, key, request)
			require.True(t, priced)
			require.InDelta(t, 0.016, hold, 1e-12)
			for _, settlement := range []struct {
				name string
				ctx  context.Context
			}{
				{name: "retry", ctx: retry},
				{name: "durable", ctx: durable},
			} {
				require.NoError(t, svc.RecordUsage(settlement.ctx, &OpenAIRecordUsageInput{
					Result: &OpenAIForwardResult{RequestID: "ungrouped-pinned-price-" + settlement.name, Model: model, BillingModel: model,
						Usage: OpenAIUsage{InputTokens: 1000, OutputTokens: 1000}},
					APIKey: key, User: user, Account: account, OriginalModel: model,
				}))
				require.NotNil(t, ledger.lastCmd)
				require.InDelta(t, 0.016, ledger.lastCmd.BalanceCost, 1e-12, settlement.name)
				usage := ledger.lastCmd.UsageLog
				require.NotNil(t, usage)
				require.Nil(t, usage.GroupID, "billing capture must not assign an ungrouped key to a group")
				require.InDelta(t, 0.002, usage.InputCost, 1e-12)
				require.InDelta(t, 0.006, usage.OutputCost, 1e-12)
				require.InDelta(t, 0.008, usage.TotalCost, 1e-12)
				require.Equal(t, 2.0, usage.RateMultiplier)
			}
			require.Equal(t, 2, ledger.calls)
			require.Zero(t, logs.calls, "the ledger already persisted each usage log atomically")
			require.Equal(t, 1, reads)

			next := svc.WithBillingPricingSnapshot(context.Background(), key, at.Add(time.Hour), model)
			hold, priced = svc.EstimateInflightReservation(next, key, request)
			require.True(t, priced)
			require.InDelta(t, tc.nextCharge, hold, 1e-12)
			require.Equal(t, 2, reads, "a new request captures the latest settings generation")
		})
	}
}

func TestUngroupedBillingSnapshot_ExplicitZeroDefaultRateSurvivesDurableRestore(t *testing.T) {
	cfg := &config.Config{}
	cfg.Default.RateMultiplier = 0
	billing := NewBillingService(cfg, nil)
	svc := &OpenAIGatewayService{cfg: cfg, billingService: billing}
	key := &APIKey{User: &User{ID: 22}}
	ctx := svc.WithBillingPricingSnapshot(context.Background(), key,
		time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC), "gpt-5.4")
	require.NoError(t, ValidateBillingPricingSnapshot(ctx))
	require.Zero(t, customerBillingRate(ctx, key, "token", 9))
	encoded, err := ExportBillingPricingSnapshot(ctx, "gpt-5.4")
	require.NoError(t, err)
	require.Contains(t, string(encoded), `"default_rate":0`)
	restored, err := RestoreBillingPricingSnapshot(context.Background(), encoded)
	require.NoError(t, err)
	require.Zero(t, customerBillingRate(restored, key, "token", 9))
	cost, err := billing.CalculateCostUnified(CostInput{Ctx: restored, Model: "gpt-5.4",
		Tokens: UsageTokens{InputTokens: 1000, OutputTokens: 1000}, RateMultiplier: 9,
		Resolver: pricingResolverForContext(restored, nil)})
	require.NoError(t, err)
	require.InDelta(t, 0.0175, cost.TotalCost, 1e-12)
	require.Zero(t, cost.ActualCost)
}

func TestUngroupedBillingSnapshot_RejectsMissingGroupAndIncompleteDurableDiscriminators(t *testing.T) {
	cfg := &config.Config{}
	cfg.Default.RateMultiplier = 2
	billing := NewBillingService(cfg, nil)
	svc := &OpenAIGatewayService{cfg: cfg, billingService: billing}
	key := &APIKey{User: &User{ID: 22}}
	ctx := svc.WithBillingPricingSnapshot(context.Background(), key,
		time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC), "gpt-5.4")
	encoded, err := ExportBillingPricingSnapshot(ctx, "gpt-5.4")
	require.NoError(t, err)
	for _, tc := range []struct {
		name string
		edit func(map[string]any)
	}{
		{name: "historical missing group", edit: func(data map[string]any) { delete(data, "ungrouped"); delete(data, "default_rate") }},
		{name: "missing discriminator", edit: func(data map[string]any) { delete(data, "ungrouped") }},
		{name: "missing rate", edit: func(data map[string]any) { delete(data, "default_rate") }},
		{name: "null rate", edit: func(data map[string]any) { data["default_rate"] = nil }},
		{name: "old version cannot claim ungrouped", edit: func(data map[string]any) { data["version"] = 1 }},
		{name: "group and ungrouped conflict", edit: func(data map[string]any) { data["group"] = map[string]any{"id": 77} }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var fields map[string]any
			require.NoError(t, json.Unmarshal(encoded, &fields))
			tc.edit(fields)
			invalid, err := json.Marshal(fields)
			require.NoError(t, err)
			_, err = RestoreBillingPricingSnapshot(context.Background(), invalid)
			require.Error(t, err)
		})
	}
	groupID := int64(77)
	missing := svc.WithBillingPricingSnapshot(context.Background(), &APIKey{GroupID: &groupID, User: key.User}, time.Now())
	require.ErrorContains(t, ValidateBillingPricingSnapshot(missing), "group snapshot unavailable")
	require.Nil(t, BillingPricingSnapshotFromContext(svc.WithBillingPricingSnapshot(nil, nil, time.Now())))
}

func TestUngroupedBillingSnapshot_DoesNotReadGroupDependenciesAndPropagatesSettingsFailure(t *testing.T) {
	billing := NewBillingService(nil, nil)
	key := &APIKey{User: &User{ID: 22}}
	personal := func(context.Context, int64, int64, float64) (float64, bool, error) {
		t.Fatal("ungrouped pricing must not read a user-group rate")
		return 0, false, nil
	}
	ctx := withBillingPricingSnapshot(WithResolvedTargetPlatform(context.Background(), PlatformOpenAI), key,
		time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC), billing, &ChannelService{}, personal, "unknown-ungrouped")
	require.NoError(t, ValidateBillingPricingSnapshot(ctx))
	require.Nil(t, BillingPricingSnapshotFromContext(ctx).channels)
	require.Nil(t, BillingPricingSnapshotFromContext(ctx).personalRate)
	billing.pricingFallbackModels = func(context.Context) (map[string]string, error) {
		return nil, errors.New("settings storage unavailable")
	}
	failed := withBillingPricingSnapshot(WithResolvedTargetPlatform(context.Background(), PlatformOpenAI), key,
		time.Now(), billing, nil, personal, "unknown-ungrouped")
	require.ErrorContains(t, ValidateBillingPricingSnapshot(failed), "settings storage unavailable")
	_, err := ExportBillingPricingSnapshot(failed, "unknown-ungrouped")
	require.Error(t, err)
}

func TestUngroupedBillingSnapshot_LegacyTaskSelectionSurvivesExportAndRestore(t *testing.T) {
	cfg := &config.Config{}
	cfg.Default.RateMultiplier = 2
	billing := NewBillingService(cfg, nil)
	svc := &OpenAIGatewayService{cfg: cfg, billingService: billing}
	key := &APIKey{User: &User{ID: 22}}
	legacy, err := RestoreBillingPricingSnapshot(context.Background(), nil)
	require.NoError(t, err)
	ctx := svc.WithBillingPricingSnapshot(legacy, key,
		time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC), "gpt-5.4")
	require.True(t, BillingPricingSnapshotFromContext(ctx).legacyPriceSelection)
	for i := 0; i < 2; i++ {
		encoded, err := ExportBillingPricingSnapshot(ctx, "gpt-5.4")
		require.NoError(t, err)
		require.Contains(t, string(encoded), `"legacy_price_selection":true`)
		ctx, err = RestoreBillingPricingSnapshot(context.Background(), encoded)
		require.NoError(t, err)
		snapshot := BillingPricingSnapshotFromContext(ctx)
		require.True(t, snapshot.ungrouped)
		require.True(t, snapshot.legacyPriceSelection)
		require.Nil(t, snapshot.group)
		require.Equal(t, BillingPeakScopeTokenOnly, snapshot.peakMeterScope())
		require.Equal(t, 2.0, customerBillingRate(ctx, key, "token", 9))
	}
}
