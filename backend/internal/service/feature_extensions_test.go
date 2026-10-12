//go:build unit || !integration

package service

// Business expectations follow docs/CHANNEL_MONITOR.md and docs/PAYMENT.md:
// source-aware observations, opt-in snapshot orders, no FX/promotions, and
// forbidden refund side effects. Expected amounts/statuses are independent
// constants from the approved examples rather than implementation calculations.

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	dbent "github.com/LuckyKuang/sub2api-plus/ent"
	"github.com/LuckyKuang/sub2api-plus/ent/paymentauditlog"
	"github.com/stretchr/testify/require"
)

func TestChannelMonitorV3ExtraProbesRuntime(t *testing.T) {
	for _, tc := range []struct {
		mode                  string
		enabled, probes, want bool
	}{
		{"v1", true, false, true}, {"v1", false, true, false}, {"v2", true, true, false}, {"v3", true, false, false}, {"v3", true, true, true}, {"v3", false, true, false},
	} {
		require.Equal(t, tc.want, (ChannelMonitorRuntime{Mode: tc.mode, Enabled: tc.enabled, V1Enabled: tc.probes}).ActiveProbesAllowed())
	}
	require.False(t, (*SettingService)(nil).GetChannelMonitorRuntime(context.Background()).ActiveProbesAllowed())
}

func TestChannelMonitorV3ProbeLatestHealthAndSource(t *testing.T) {
	cfg := DefaultChannelMonitorV3Config()
	cfg.MinimumSamples = 100
	for status, want := range map[string]string{"operational": "normal", "degraded": "degraded", "failed": "outage", "error": "outage", "": "unknown"} {
		require.Equal(t, want, ChannelMonitorV3Health(ChannelMonitorV3Fact{SourceKind: "probe", ProbeStatus: status, Success: 1}, cfg))
	}
	require.NotEqual(t, ChannelMonitorV3Scope("openai", 1, "model"), ChannelMonitorV3SourceScope("openai", "probe", 0, 1, "model"))
	raw, err := json.Marshal(ChannelMonitorV3Model{SourceKind: "probe", MonitorID: featurePtrInt64(1)})
	require.NoError(t, err)
	require.Contains(t, string(raw), `"group_id":null`)
	require.Contains(t, string(raw), `"monitor_id":1`)
	for _, tc := range []struct {
		statuses []string
		want     string
	}{
		{[]string{"normal", "outage"}, "partial"}, {[]string{"outage", "outage"}, "outage"}, {[]string{"normal", "degraded"}, "degraded"}, {[]string{"normal", "normal"}, "normal"},
	} {
		models := []ChannelMonitorV3Model{}
		for _, status := range tc.statuses {
			models = append(models, ChannelMonitorV3Model{Status: status})
		}
		require.Equal(t, tc.want, CombineChannelMonitorV3Scopes(models, "unknown"))
	}
	now := time.Now().UTC()
	fact := ChannelMonitorV3Fact{Platform: "openai", SourceKind: "probe", MonitorID: 1, Model: "model", ProbeStatus: "failed", Failures: 1, LastRequest: now}
	state, event := AdvanceChannelMonitorV3State(ChannelMonitorV3State{}, fact, now, cfg)
	require.Nil(t, event)
	state, event = AdvanceChannelMonitorV3State(state, fact, now.Add(time.Minute), cfg)
	require.Nil(t, event)
	require.Equal(t, 1, state.Streak)
	fact.LastRequest = now.Add(2 * time.Minute)
	state, event = AdvanceChannelMonitorV3State(state, fact, now.Add(2*time.Minute), cfg)
	require.NotNil(t, event)
	require.Equal(t, "probe", event.SourceKind)
	_, event = AdvanceChannelMonitorV3State(state, ChannelMonitorV3Fact{SourceKind: "probe"}, now.Add(8*time.Minute), cfg)
	require.Equal(t, "awaiting_data", event.Phase)
}

func TestPaymentRedeemPurchaseValidationAndAtomicPatch(t *testing.T) {
	for _, raw := range []string{"javascript:alert(1)", "//shop.example/path", "https://user:pass@shop.example/", "https://shop.example/\n", "https:///path"} {
		require.Error(t, ValidateRedeemPurchase(true, raw, "iframe"))
	}
	require.Error(t, ValidateRedeemPurchase(true, "", "new_tab"))
	require.Error(t, ValidateRedeemPurchase(false, "https://shop.example", "invalid"))
	require.NoError(t, ValidateRedeemPurchase(false, "", "new_tab"))
	require.NoError(t, ValidateRedeemPurchase(true, "https://shop.example/path?product=code#buy", "iframe"))
	repo := &paymentConfigSettingRepoStub{values: map[string]string{SettingPaymentRedeemPurchaseURL: "https://shop.example/path", SettingPaymentRedeemPurchaseEnabled: "true", SettingPaymentRedeemPurchaseMode: "iframe"}}
	svc := NewPaymentConfigService(nil, repo, nil)
	invalid := "javascript:alert(1)"
	enabled := false
	require.Error(t, svc.UpdatePaymentConfig(context.Background(), UpdatePaymentConfigRequest{Enabled: &enabled, RedeemPurchaseURL: &invalid}))
	require.Empty(t, repo.updates)
	called := 0
	svc.SetOnUpdateCallback(func() { called++ })
	mode := "new_tab"
	require.NoError(t, svc.UpdatePaymentConfig(context.Background(), UpdatePaymentConfigRequest{RedeemPurchaseMode: &mode}))
	require.Equal(t, 1, called)
}

func featurePtrInt64(value int64) *int64 { return &value }

// Approved requirement: 100 CNY redeem + 50 CNY payment + 20 USD payment
// reports three orders and separate CNY 150 / USD 20 totals, without FX.
func TestPaymentRedeemSourceReportingAndActualPaymentLimits(t *testing.T) {
	ctx := context.Background()
	client := newPaymentConfigServiceTestClient(t)
	require.NoError(t, client.Schema.Create(ctx))
	user, err := client.User.Create().SetEmail("source-report@example.com").SetPasswordHash("test").Save(ctx)
	require.NoError(t, err)
	now := time.Now()
	for _, row := range []struct {
		source, currency, kind string
		amount                 float64
	}{
		{"redeem", "CNY", "subscription", 100},
		{"payment", "CNY", "balance", 50},
		{"payment", "USD", "balance", 20},
	} {
		_, err := client.PaymentOrder.Create().SetUserID(user.ID).SetUserEmail(user.Email).SetUserName("").
			SetAmount(row.amount).SetPayAmount(row.amount).SetRechargeCode("").SetPaymentType(row.source).
			SetPaymentTradeNo("").SetOrderSource(row.source).SetOrderType(row.kind).SetCurrency(row.currency).
			SetStatus(OrderStatusCompleted).SetPaidAt(now).SetExpiresAt(now).SetClientIP("").SetSrcHost("").Save(ctx)
		require.NoError(t, err)
	}
	svc := &PaymentService{entClient: client}
	stats, err := svc.GetDashboardStats(ctx, 30)
	require.NoError(t, err)
	require.Equal(t, 3, stats.TotalCount)
	require.Equal(t, CurrencyAmounts{"CNY": 150, "USD": 20}, stats.TotalAmount)
	stats, err = svc.GetDashboardStats(ctx, 30, "redeem")
	require.NoError(t, err)
	require.Equal(t, 1, stats.TotalCount)
	require.Equal(t, CurrencyAmounts{"CNY": 100}, stats.TotalAmount)
	stats, err = svc.GetDashboardStats(ctx, 30, "payment")
	require.NoError(t, err)
	require.Equal(t, 2, stats.TotalCount)
	require.Equal(t, CurrencyAmounts{"CNY": 50, "USD": 20}, stats.TotalAmount)
	orders, total, err := svc.GetUserOrders(ctx, user.ID, OrderListParams{OrderSource: "redeem", OrderType: "subscription"})
	require.NoError(t, err)
	require.Equal(t, 1, total)
	require.Len(t, orders, 1)
	_, total, err = svc.AdminListOrders(ctx, user.ID, OrderListParams{OrderSource: "redeem", OrderType: "balance"})
	require.NoError(t, err)
	require.Zero(t, total)
	_, _, err = svc.GetUserOrders(ctx, user.ID, OrderListParams{OrderSource: "unknown"})
	require.Error(t, err)
	_, _, err = svc.AdminListOrders(ctx, user.ID, OrderListParams{OrderSource: "unknown"})
	require.Error(t, err)
	_, err = svc.GetDashboardStats(ctx, 30, "unknown")
	require.Error(t, err)

	// A redeem order in a different currency does not consume payment limits
	// or cause a payment currency-mismatch rejection.
	limitUser, err := client.User.Create().SetEmail("redeem-limit@example.com").SetPasswordHash("test").Save(ctx)
	require.NoError(t, err)
	_, err = client.PaymentOrder.Create().SetUserID(limitUser.ID).SetUserEmail(limitUser.Email).SetUserName("").
		SetAmount(100).SetPayAmount(100).SetRechargeCode("").SetPaymentType("redeem").SetPaymentTradeNo("").
		SetOrderSource("redeem").SetCurrency("USD").SetStatus(OrderStatusCompleted).SetPaidAt(now).
		SetExpiresAt(now).SetClientIP("").SetSrcHost("").Save(ctx)
	require.NoError(t, err)
	tx, err := client.Tx(ctx)
	require.NoError(t, err)
	defer func() { _ = tx.Rollback() }()
	require.NoError(t, svc.checkDailyLimit(ctx, tx, limitUser.ID, 50, 50, "CNY"))
}

func TestPaymentRedeemPurchaseDefaultsAndExactFrameOrigin(t *testing.T) {
	ctx := context.Background()
	repo := &paymentConfigSettingRepoStub{values: map[string]string{}}
	cfg, err := NewPaymentConfigService(nil, repo, nil).GetPaymentConfig(ctx)
	require.NoError(t, err)
	require.False(t, cfg.RedeemPurchaseEnabled)
	require.Empty(t, cfg.RedeemPurchaseURL)
	require.Equal(t, "new_tab", cfg.RedeemPurchaseMode)
	settings := NewSettingService(repo, nil)
	for _, tc := range []struct {
		payment, enabled, mode, url string
		want                        []string
	}{
		{"true", "true", "iframe", "https://shop.example:8443/code?item=100#buy", []string{"https://shop.example:8443"}},
		{"false", "true", "iframe", "https://shop.example/code", nil},
		{"true", "false", "iframe", "https://shop.example/code", nil},
		{"true", "true", "new_tab", "https://shop.example/code", nil},
		{"true", "true", "iframe", "https://user:password@shop.example/code", nil},
	} {
		repo.values = map[string]string{SettingPaymentEnabled: tc.payment, SettingPaymentRedeemPurchaseEnabled: tc.enabled,
			SettingPaymentRedeemPurchaseMode: tc.mode, SettingPaymentRedeemPurchaseURL: tc.url}
		origins, err := settings.GetFrameSrcOrigins(ctx)
		require.NoError(t, err)
		require.Equal(t, tc.want, origins)
	}
}

func TestChannelMonitorV3SnapshotCannotHideProbeFailureWithTraffic(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Minute)
	cfg := DefaultChannelMonitorV3Config()
	data := &ChannelMonitorV3Data{DataThrough: &now, CurrentThrough: &now, States: map[string]ChannelMonitorV3State{},
		Catalog: []ChannelMonitorV3Catalog{{Platform: "openai", GroupID: 1}, {Platform: "openai", SourceKind: "probe", MonitorID: 1}},
		Current: []ChannelMonitorV3Fact{
			{Platform: "openai", GroupID: 1, Model: "model", Success: 10000, LastRequest: now.Add(-time.Second)},
			{Platform: "openai", SourceKind: "probe", MonitorID: 1, Model: "model", Failures: 1, ProbeStatus: "failed", LastRequest: now.Add(-time.Second)},
		}}
	result := BuildChannelMonitorV3Snapshot(data, cfg, now, 24*time.Hour, "")
	require.Len(t, result.Platforms, 1)
	require.Equal(t, "partial", result.Platforms[0].Status)
	require.Len(t, result.Platforms[0].Models, 2)
	data.Current[0].Success, data.Current[0].Failures = 0, 10000
	result = BuildChannelMonitorV3Snapshot(data, cfg, now, 24*time.Hour, "")
	require.Equal(t, "outage", result.Platforms[0].Status)
}

// Approved monitor spec: active observations are sufficient independently of
// the real-request minimum. Historical rates use the combined observation
// count, while probe latency must never become a TTFT sample.
func TestChannelMonitorV3ProbeHistoryDoesNotRequireTrafficMinimum(t *testing.T) {
	now := time.Date(2026, 10, 11, 12, 0, 0, 0, time.UTC)
	cfg := DefaultChannelMonitorV3Config()
	cfg.MinimumSamples = 100
	for _, tc := range []struct {
		name     string
		history  []ChannelMonitorV3Fact
		wantRate float64
	}{
		{"one healthy probe", []ChannelMonitorV3Fact{{Platform: "openai", SourceKind: "probe", MonitorID: 1, Model: "model", Success: 1}}, 1},
		{"one failing probe", []ChannelMonitorV3Fact{{Platform: "openai", SourceKind: "probe", MonitorID: 1, Model: "model", Failures: 1}}, 0},
		{"one probe and one failed request", []ChannelMonitorV3Fact{{Platform: "openai", SourceKind: "probe", MonitorID: 1, Model: "model", Success: 1}, {Platform: "openai", GroupID: 2, Model: "model", Failures: 1}}, 0.5},
	} {
		t.Run(tc.name, func(t *testing.T) {
			data := &ChannelMonitorV3Data{DataThrough: &now, Catalog: []ChannelMonitorV3Catalog{{Platform: "openai", SourceKind: "probe", MonitorID: 1}, {Platform: "openai", GroupID: 2}}, Totals: tc.history}
			result := BuildChannelMonitorV3Snapshot(data, cfg, now, 24*time.Hour, "openai")
			require.Len(t, result.Platforms, 1)
			require.NotNil(t, result.Platforms[0].SuccessRate, "a configured probe is independent of the traffic sample minimum")
			require.Equal(t, tc.wantRate, *result.Platforms[0].SuccessRate)
			require.Nil(t, result.Platforms[0].TTFTP50Ms)
		})
	}
	t.Run("configured monitor without probe observations keeps the traffic minimum", func(t *testing.T) {
		data := &ChannelMonitorV3Data{DataThrough: &now, Catalog: []ChannelMonitorV3Catalog{{Platform: "openai", SourceKind: "probe", MonitorID: 1}, {Platform: "openai", GroupID: 2}},
			Totals: []ChannelMonitorV3Fact{{Platform: "openai", SourceKind: "probe", MonitorID: 1}, {Platform: "openai", GroupID: 2, Success: 1}}}
		result := BuildChannelMonitorV3Snapshot(data, cfg, now, 24*time.Hour, "openai")
		require.Len(t, result.Platforms, 1)
		require.Nil(t, result.Platforms[0].SuccessRate)
		require.Nil(t, result.Platforms[0].TTFTP50Ms)
	})
}

func TestRedeemOrderBalanceNoConversionAndRefundGuard(t *testing.T) {
	ctx := context.Background()
	client := newPaymentConfigServiceTestClient(t)
	require.NoError(t, client.Schema.Create(ctx))
	user, err := client.User.Create().SetEmail("redeem-order@example.com").SetPasswordHash("test").Save(ctx)
	require.NoError(t, err)
	code := &RedeemCode{ID: 42, Type: RedeemTypeBalance, Value: 100, GenerateOrder: true, OrderCurrency: "CNY"}
	require.NoError(t, freezeRedeemOrder(ctx, client, code))
	require.Equal(t, 100.0, code.Value)
	require.Equal(t, 100.0, code.OrderAmount)
	tx, err := client.Tx(ctx)
	require.NoError(t, err)
	require.NoError(t, createRedeemOrder(ctx, tx, code, user.ID))
	require.NoError(t, tx.Commit())
	order, err := client.PaymentOrder.Query().Only(ctx)
	require.NoError(t, err)
	require.Equal(t, "redeem", order.OrderSource)
	require.Equal(t, "COMPLETED", order.Status)
	require.Equal(t, 100.0, order.PayAmount)
	require.Equal(t, "CNY", order.Currency)
	require.Empty(t, order.ProviderSnapshot)
	require.Empty(t, order.ProviderInstanceID)
	require.Empty(t, order.RechargeCode)
	require.Zero(t, order.BonusAmount)
	require.Zero(t, order.FeeRate)
	logs, err := client.PaymentAuditLog.Query().Where(paymentauditlog.ActionEQ("REDEEM_ORDER_COMPLETED")).All(ctx)
	require.NoError(t, err)
	require.Len(t, logs, 1)
	svc := &PaymentService{entClient: client}
	err = svc.RequestRefund(ctx, order.ID, user.ID, "test")
	require.ErrorIs(t, err, ErrRedeemOrderRefundDisabled)
	_, _, err = svc.PrepareRefund(ctx, order.ID, 10, "test", true, true)
	require.ErrorIs(t, err, ErrRedeemOrderRefundDisabled)
	_, err = svc.ExecuteRefund(ctx, &RefundPlan{OrderID: order.ID, Order: &dbent.PaymentOrder{OrderSource: "payment"}, Force: true})
	require.ErrorIs(t, err, ErrRedeemOrderRefundDisabled)
	_, err = svc.QueryAndFinalizeRefund(ctx, order.ID)
	require.ErrorIs(t, err, ErrRedeemOrderRefundDisabled)
	unchanged, err := client.PaymentOrder.Get(ctx, order.ID)
	require.NoError(t, err)
	require.Equal(t, "COMPLETED", unchanged.Status)
	require.Zero(t, unchanged.RefundAmount)
}

func TestRedeemOrderSubscriptionSnapshotSurvivesPlanChanges(t *testing.T) {
	ctx := context.Background()
	client := newPaymentConfigServiceTestClient(t)
	require.NoError(t, client.Schema.Create(ctx))
	group, err := client.Group.Create().SetName("subscription").SetSubscriptionType("subscription").Save(ctx)
	require.NoError(t, err)
	plan, err := client.SubscriptionPlan.Create().SetName("Original").SetGroupID(group.ID).SetPrice(19).SetOriginalPrice(99).SetCurrency("USD").SetValidityDays(2).SetValidityUnit("week").SetForSale(false).Save(ctx)
	require.NoError(t, err)
	user, err := client.User.Create().SetEmail("frozen-plan@example.com").SetPasswordHash("test").Save(ctx)
	require.NoError(t, err)
	code := &RedeemCode{ID: 42, GenerateOrder: true, Type: RedeemTypeSubscription, OrderPlanID: &plan.ID}
	require.NoError(t, freezeRedeemOrder(ctx, client, code))
	require.Equal(t, 19.0, code.OrderAmount)
	require.Equal(t, 14, code.ValidityDays)
	require.Equal(t, "Original", code.OrderPlanName)
	_, err = client.SubscriptionPlan.UpdateOneID(plan.ID).SetName("Changed").SetPrice(999).SetCurrency("CNY").SetValidityDays(1).Save(ctx)
	require.NoError(t, err)
	require.NoError(t, client.SubscriptionPlan.DeleteOneID(plan.ID).Exec(ctx))
	tx, err := client.Tx(ctx)
	require.NoError(t, err)
	defer func() { _ = tx.Rollback() }()
	require.NoError(t, createRedeemOrder(ctx, tx, code, user.ID))
	require.NoError(t, tx.Commit())
	order, err := client.PaymentOrder.Query().Only(ctx)
	require.NoError(t, err)
	require.Equal(t, 19.0, order.Amount)
	require.Equal(t, 19.0, order.PayAmount)
	require.Equal(t, "USD", order.Currency)
	require.Equal(t, "Original", order.PlanName)
	require.Equal(t, 14, *order.SubscriptionDays)
	require.Equal(t, group.ID, *order.SubscriptionGroupID)
	for _, code := range []*RedeemCode{{GenerateOrder: true, Type: RedeemTypeConcurrency, Value: 10}, {GenerateOrder: true, Type: RedeemTypeBalance, Value: 100, OrderCurrency: "EUR"}, {GenerateOrder: true, Type: RedeemTypeBalance, Value: -1}, {GenerateOrder: true, Type: RedeemTypeSubscription}} {
		require.Error(t, freezeRedeemOrder(ctx, client, code))
	}
}

func TestRedeemOrderSubscriptionRejectsConflictingEntitlement(t *testing.T) {
	ctx := context.Background()
	client := newPaymentConfigServiceTestClient(t)
	require.NoError(t, client.Schema.Create(ctx))
	group, err := client.Group.Create().SetName("subscription").SetPlatform("openai").SetSubscriptionType("subscription").Save(ctx)
	require.NoError(t, err)
	plan, err := client.SubscriptionPlan.Create().SetGroupID(group.ID).SetName("month").SetPrice(25).SetValidityDays(1).SetValidityUnit("month").Save(ctx)
	require.NoError(t, err)
	for _, code := range []*RedeemCode{
		{GenerateOrder: true, Type: RedeemTypeSubscription, OrderPlanID: &plan.ID, GroupID: featurePtrInt64(group.ID + 1)},
		{GenerateOrder: true, Type: RedeemTypeSubscription, OrderPlanID: &plan.ID, ValidityDays: 1},
		{GenerateOrder: true, Type: RedeemTypeSubscription, OrderPlanID: &plan.ID, ValidityDays: -30},
	} {
		require.Error(t, freezeRedeemOrder(ctx, client, code))
	}
	code := &RedeemCode{GenerateOrder: true, Type: RedeemTypeSubscription, OrderPlanID: &plan.ID, GroupID: &group.ID, ValidityDays: 30, Value: 999}
	require.NoError(t, freezeRedeemOrder(ctx, client, code))
	require.Equal(t, 25.0, code.OrderAmount)
	require.Equal(t, "CNY", code.OrderCurrency)
	require.Equal(t, 30, code.ValidityDays)
}
