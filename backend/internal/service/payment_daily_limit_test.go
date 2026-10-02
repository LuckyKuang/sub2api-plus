//go:build unit

package service

import (
	"context"
	"testing"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/payment"
	infraerrors "github.com/LuckyKuang/sub2api-plus/internal/pkg/errors"
	"github.com/stretchr/testify/require"
)

func TestCheckDailyLimitUsesActualPaidAmountNotCreditedBalance(t *testing.T) {
	ctx := context.Background()
	client := newPaymentConfigServiceTestClient(t)

	user, err := client.User.Create().
		SetEmail("daily-limit@example.com").
		SetPasswordHash("hash").
		SetUsername("daily-limit-user").
		Save(ctx)
	require.NoError(t, err)

	// An incentivized order: 130 USD credited, of which 30 USD is free, 100 USD paid.
	_, err = client.PaymentOrder.Create().
		SetUserID(user.ID).
		SetUserEmail(user.Email).
		SetUserName(user.Username).
		SetAmount(130).
		SetPayAmount(100).
		SetBonusAmount(30).
		SetFeeRate(0).
		SetRechargeCode("DAILY-LIMIT-ORDER").
		SetOutTradeNo("daily_limit_order").
		SetPaymentType(payment.TypeStripe).
		SetPaymentTradeNo("pi_daily_limit").
		SetOrderType(payment.OrderTypeBalance).
		SetStatus(OrderStatusCompleted).
		SetExpiresAt(time.Now().Add(time.Hour)).
		SetPaidAt(time.Now()).
		SetClientIP("127.0.0.1").
		SetSrcHost("api.example.com").
		SetProviderSnapshot(map[string]any{"schema_version": 2, "provider_key": payment.TypeStripe, "currency": "USD"}).
		Save(ctx)
	require.NoError(t, err)

	svc := &PaymentService{entClient: client}
	tx, err := client.Tx(ctx)
	require.NoError(t, err)
	defer func() { _ = tx.Rollback() }()

	// 100 paid + 5 stays under 110; the 130 credited must not be used (135 > 110).
	require.NoError(t, svc.checkDailyLimit(ctx, tx, user.ID, 5, 110, "USD"))

	err = svc.checkDailyLimit(ctx, tx, user.ID, 15, 110, "USD")
	require.Error(t, err)
	require.Equal(t, "DAILY_LIMIT_EXCEEDED", infraerrors.Reason(err))
}

func TestCheckDailyLimitNoIncentiveRegression(t *testing.T) {
	ctx := context.Background()
	client := newPaymentConfigServiceTestClient(t)

	user, err := client.User.Create().
		SetEmail("daily-limit-plain@example.com").
		SetPasswordHash("hash").
		SetUsername("daily-limit-plain").
		Save(ctx)
	require.NoError(t, err)

	// No-incentive case: credited == paid + fees, bonus 0.
	_, err = client.PaymentOrder.Create().
		SetUserID(user.ID).
		SetUserEmail(user.Email).
		SetUserName(user.Username).
		SetAmount(50).
		SetPayAmount(51.5).
		SetBonusAmount(0).
		SetFeeRate(3).
		SetRechargeCode("DAILY-LIMIT-PLAIN").
		SetOutTradeNo("daily_limit_plain").
		SetPaymentType(payment.TypeStripe).
		SetPaymentTradeNo("pi_daily_limit").
		SetOrderType(payment.OrderTypeBalance).
		SetStatus(OrderStatusCompleted).
		SetExpiresAt(time.Now().Add(time.Hour)).
		SetPaidAt(time.Now()).
		SetClientIP("127.0.0.1").
		SetSrcHost("api.example.com").
		SetProviderSnapshot(map[string]any{"schema_version": 2, "provider_key": payment.TypeStripe, "currency": "USD"}).
		Save(ctx)
	require.NoError(t, err)

	svc := &PaymentService{entClient: client}
	tx, err := client.Tx(ctx)
	require.NoError(t, err)
	defer func() { _ = tx.Rollback() }()

	// Fees are part of the basis: 51.5 + 48.5 == 100 is allowed, 51.5 + 48.6 is not.
	require.NoError(t, svc.checkDailyLimit(ctx, tx, user.ID, 48.5, 100, "USD"))
	err = svc.checkDailyLimit(ctx, tx, user.ID, 48.6, 100, "USD")
	require.Error(t, err)
	require.Equal(t, "DAILY_LIMIT_EXCEEDED", infraerrors.Reason(err))
}

func TestCheckDailyLimitRejectsUnreliableCurrencyMix(t *testing.T) {
	ctx := context.Background()
	client := newPaymentConfigServiceTestClient(t)

	user, err := client.User.Create().
		SetEmail("daily-limit-currency@example.com").
		SetPasswordHash("hash").
		SetUsername("daily-limit-currency").
		Save(ctx)
	require.NoError(t, err)

	_, err = client.PaymentOrder.Create().
		SetUserID(user.ID).
		SetUserEmail(user.Email).
		SetUserName(user.Username).
		SetAmount(100).
		SetPayAmount(100).
		SetBonusAmount(0).
		SetFeeRate(0).
		SetRechargeCode("DAILY-LIMIT-CURRENCY").
		SetOutTradeNo("daily_limit_currency").
		SetPaymentType(payment.TypeStripe).
		SetPaymentTradeNo("pi_daily_limit").
		SetOrderType(payment.OrderTypeBalance).
		SetStatus(OrderStatusCompleted).
		SetExpiresAt(time.Now().Add(time.Hour)).
		SetPaidAt(time.Now()).
		SetClientIP("127.0.0.1").
		SetSrcHost("api.example.com").
		SetProviderSnapshot(map[string]any{"schema_version": 2, "provider_key": payment.TypeStripe, "currency": "USD"}).
		Save(ctx)
	require.NoError(t, err)

	svc := &PaymentService{entClient: client}
	tx, err := client.Tx(ctx)
	require.NoError(t, err)
	defer func() { _ = tx.Rollback() }()

	err = svc.checkDailyLimit(ctx, tx, user.ID, 1, 1000, "CNY")
	require.Error(t, err)
	require.Equal(t, "PAYMENT_DAILY_LIMIT_CURRENCY_MISMATCH", infraerrors.Reason(err))
}

func TestCheckDailyLimitIgnoresSubscriptionsUnchangedBasis(t *testing.T) {
	ctx := context.Background()
	client := newPaymentConfigServiceTestClient(t)

	user, err := client.User.Create().
		SetEmail("daily-limit-sub@example.com").
		SetPasswordHash("hash").
		SetUsername("daily-limit-sub").
		Save(ctx)
	require.NoError(t, err)

	// Subscription orders keep the plan-price basis regardless of the payment currency.
	_, err = client.PaymentOrder.Create().
		SetUserID(user.ID).
		SetUserEmail(user.Email).
		SetUserName(user.Username).
		SetAmount(20).
		SetPayAmount(140).
		SetBonusAmount(0).
		SetFeeRate(0).
		SetRechargeCode("DAILY-LIMIT-SUB").
		SetOutTradeNo("daily_limit_sub").
		SetPaymentType(payment.TypeStripe).
		SetPaymentTradeNo("pi_daily_limit").
		SetOrderType(payment.OrderTypeSubscription).
		SetStatus(OrderStatusCompleted).
		SetExpiresAt(time.Now().Add(time.Hour)).
		SetPaidAt(time.Now()).
		SetClientIP("127.0.0.1").
		SetSrcHost("api.example.com").
		Save(ctx)
	require.NoError(t, err)

	svc := &PaymentService{entClient: client}
	tx, err := client.Tx(ctx)
	require.NoError(t, err)
	defer func() { _ = tx.Rollback() }()

	// 20 (plan price, not 140 collected) + 79 <= 100.
	require.NoError(t, svc.checkDailyLimit(ctx, tx, user.ID, 79, 100, "CNY"))
}
