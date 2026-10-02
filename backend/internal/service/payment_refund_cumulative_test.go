//go:build unit

package service

import (
	"context"
	"strconv"
	"testing"
	"time"

	dbent "github.com/LuckyKuang/sub2api-plus/ent"
	"github.com/LuckyKuang/sub2api-plus/internal/payment"
	"github.com/stretchr/testify/require"
)

// createRefundableOrder creates a completed balance order bound to a refundable
// Stripe instance and a frozen currency snapshot.
func createRefundableOrder(t *testing.T, ctx context.Context, client *dbent.Client, suffix string, amount, payAmount, bonus float64, currency string) *dbent.PaymentOrder {
	t.Helper()

	user, err := client.User.Create().
		SetEmail("refund-incentive-" + suffix + "@example.com").
		SetPasswordHash("hash").
		SetUsername("refund-incentive-" + suffix).
		SetBalance(10000).
		Save(ctx)
	require.NoError(t, err)

	inst, err := client.PaymentProviderInstance.Create().
		SetProviderKey(payment.TypeStripe).
		SetName("refund-incentive-" + suffix).
		SetConfig("{}").
		SetSupportedTypes("stripe").
		SetEnabled(true).
		SetRefundEnabled(true).
		SetAllowUserRefund(true).
		Save(ctx)
	require.NoError(t, err)

	instID := strconv.FormatInt(inst.ID, 10)
	order, err := client.PaymentOrder.Create().
		SetUserID(user.ID).
		SetUserEmail(user.Email).
		SetUserName(user.Username).
		SetAmount(amount).
		SetPayAmount(payAmount).
		SetBonusAmount(bonus).
		SetFeeRate(0).
		SetRechargeCode("REFUND-INCENTIVE-" + suffix).
		SetOutTradeNo("sub2_refund_incentive_" + suffix).
		SetPaymentType(payment.TypeStripe).
		SetPaymentTradeNo("pi_refund_incentive_" + suffix).
		SetOrderType(payment.OrderTypeBalance).
		SetStatus(OrderStatusCompleted).
		SetExpiresAt(time.Now().Add(time.Hour)).
		SetPaidAt(time.Now()).
		SetClientIP("127.0.0.1").
		SetSrcHost("api.example.com").
		SetProviderInstanceID(instID).
		SetProviderKey(payment.TypeStripe).
		SetProviderSnapshot(map[string]any{
			"schema_version":       2,
			"provider_instance_id": instID,
			"provider_key":         payment.TypeStripe,
			"currency":             currency,
		}).
		Save(ctx)
	require.NoError(t, err)
	return order
}

func refundServiceForIncentive(client *dbent.Client) *PaymentService {
	return &PaymentService{
		entClient: client,
		userRepo:  &mockUserRepo{getByIDUser: &User{Balance: 10000}},
	}
}

func TestRefundSequentialPartialsCapCashAtPaidAmountWithBonus(t *testing.T) {
	ctx := context.Background()
	client := newPaymentConfigServiceTestClient(t)
	// 100 USD paid buys 130 USD credited (30 USD free).
	order := createRefundableOrder(t, ctx, client, "bonus-split", 130, 100, 30, "USD")
	svc := refundServiceForIncentive(client)

	first, early, err := svc.PrepareRefund(ctx, order.ID, 65, "half", false, true)
	require.NoError(t, err)
	require.Nil(t, early)
	require.Equal(t, 65.0, first.RefundAmount)
	require.Equal(t, 65.0, first.CumulativeCredited)
	require.InDelta(t, 50.0, first.GatewayAmount, 1e-9, "cash follows the paid/credited ratio")
	require.Equal(t, 65.0, first.BalanceToDeduct)

	_, err = svc.markRefundOk(ctx, first)
	require.NoError(t, err)
	reloaded, err := client.PaymentOrder.Get(ctx, order.ID)
	require.NoError(t, err)
	require.Equal(t, OrderStatusPartiallyRefunded, reloaded.Status)
	require.Equal(t, 65.0, reloaded.RefundAmount)

	// The remaining credited balance is refundable as a second partial refund.
	second, early, err := svc.PrepareRefund(ctx, order.ID, 0, "rest", false, true)
	require.NoError(t, err)
	require.Nil(t, early)
	require.Equal(t, 65.0, second.RefundAmount)
	require.Equal(t, 130.0, second.CumulativeCredited)
	require.InDelta(t, 50.0, second.GatewayAmount, 1e-9)

	_, err = svc.markRefundOk(ctx, second)
	require.NoError(t, err)
	reloaded, err = client.PaymentOrder.Get(ctx, order.ID)
	require.NoError(t, err)
	require.Equal(t, OrderStatusRefunded, reloaded.Status)
	require.Equal(t, 130.0, reloaded.RefundAmount)

	require.InDelta(t, 100.0, first.GatewayAmount+second.GatewayAmount, 1e-9, "cumulative cash never exceeds the original pay amount")
	require.Equal(t, 130.0, first.BalanceToDeduct+second.BalanceToDeduct, "credited balance is clawed back in full")

	// Nothing remains.
	_, _, err = svc.PrepareRefund(ctx, order.ID, 1, "extra", false, true)
	require.Error(t, err)
}

func TestRefundCumulativeCashUsesCurrencyPrecision(t *testing.T) {
	ctx := context.Background()
	client := newPaymentConfigServiceTestClient(t)
	// Three-decimal currency: 12.345 collected for 100 credited.
	order := createRefundableOrder(t, ctx, client, "kwd-split", 100, 12.345, 0, "KWD")
	require.Equal(t, "KWD", PaymentOrderCurrency(order), "order must carry the frozen provider currency")
	require.InDelta(t, 12.345, order.PayAmount, 1e-12)
	svc := refundServiceForIncentive(client)

	first, _, err := svc.PrepareRefund(ctx, order.ID, 50, "half", false, true)
	require.NoError(t, err)
	require.InDelta(t, 6.173, first.GatewayAmount, 1e-12)
	_, err = svc.markRefundOk(ctx, first)
	require.NoError(t, err)

	second, _, err := svc.PrepareRefund(ctx, order.ID, 0, "rest", false, true)
	require.NoError(t, err)
	require.InDelta(t, 6.172, second.GatewayAmount, 1e-12)
	_, err = svc.markRefundOk(ctx, second)
	require.NoError(t, err)

	require.InDelta(t, 12.345, first.GatewayAmount+second.GatewayAmount, 1e-12)
}

func TestRefundHistoricalOrderWithoutBonusKeepsPriorBehavior(t *testing.T) {
	ctx := context.Background()
	client := newPaymentConfigServiceTestClient(t)
	order := createRefundableOrder(t, ctx, client, "legacy", 50, 50, 0, "USD")
	svc := refundServiceForIncentive(client)

	plan, _, err := svc.PrepareRefund(ctx, order.ID, 0, "full", false, true)
	require.NoError(t, err)
	require.Equal(t, 50.0, plan.RefundAmount)
	require.Equal(t, 50.0, plan.GatewayAmount)
	require.Equal(t, 50.0, plan.BalanceToDeduct)

	_, err = svc.markRefundOk(ctx, plan)
	require.NoError(t, err)
	reloaded, err := client.PaymentOrder.Get(ctx, order.ID)
	require.NoError(t, err)
	require.Equal(t, OrderStatusRefunded, reloaded.Status)
}

func TestRefundPartialThenRestPreservesPendingDetailBasis(t *testing.T) {
	ctx := context.Background()
	client := newPaymentConfigServiceTestClient(t)
	order := createRefundableOrder(t, ctx, client, "pending-cumulative", 130, 100, 30, "USD")
	svc := refundServiceForIncentive(client)

	plan, _, err := svc.PrepareRefund(ctx, order.ID, 65, "pending half", false, true)
	require.NoError(t, err)

	// The gateway accepts but has not finalized yet: cumulative state is persisted.
	result, err := svc.markRefundPending(ctx, plan, &payment.RefundResponse{RefundID: "rf_half", Status: payment.ProviderStatusPending})
	require.NoError(t, err)
	require.False(t, result.Success)

	reloaded, err := client.PaymentOrder.Get(ctx, order.ID)
	require.NoError(t, err)
	require.Equal(t, OrderStatusRefundPending, reloaded.Status)
	require.Equal(t, 65.0, reloaded.RefundAmount, "the pending credited amount is observable")

	finalPlan := svc.refundFinalizePlan(ctx, reloaded)
	require.Equal(t, 65.0, finalPlan.RefundAmount)
	require.Equal(t, 65.0, finalPlan.CumulativeCredited)
	require.InDelta(t, 50.0, finalPlan.GatewayAmount, 1e-9)

	finalized, err := svc.finalizePendingRefundSuccess(ctx, finalPlan)
	require.NoError(t, err)
	require.True(t, finalized.Success)

	reloaded, err = client.PaymentOrder.Get(ctx, order.ID)
	require.NoError(t, err)
	require.Equal(t, OrderStatusPartiallyRefunded, reloaded.Status)
	require.Equal(t, 65.0, reloaded.RefundAmount)

	// The remaining half stays refundable.
	_, _, err = svc.PrepareRefund(ctx, order.ID, 1, "almost done", false, true)
	require.NoError(t, err)
}

func TestRefundRequestDoesNotConsumeRefundableAmount(t *testing.T) {
	ctx := context.Background()
	client := newPaymentConfigServiceTestClient(t)
	order := createRefundableOrder(t, ctx, client, "user-request", 130, 100, 30, "USD")
	svc := refundServiceForIncentive(client)

	require.NoError(t, svc.RequestRefund(ctx, order.ID, order.UserID, "user asked"))
	reloaded, err := client.PaymentOrder.Get(ctx, order.ID)
	require.NoError(t, err)
	require.Equal(t, OrderStatusRefundRequested, reloaded.Status)
	require.Zero(t, reloaded.RefundAmount)

	plan, _, err := svc.PrepareRefund(ctx, order.ID, 0, "admin approves", false, true)
	require.NoError(t, err)
	require.Equal(t, 130.0, plan.RefundAmount)
	require.InDelta(t, 100.0, plan.GatewayAmount, 1e-9)
}
