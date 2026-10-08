//go:build unit

package service

import (
	"context"
	"net/url"
	"strconv"
	"testing"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/payment"
	"github.com/stretchr/testify/require"
)

func TestPaymentProviderInvocationStripsClientQueryAndPreservesResume(t *testing.T) {
	ctx := context.Background()
	client := newPaymentConfigServiceTestClient(t)
	user := client.User.Create().SetEmail("checkout@example.com").SetPasswordHash("hash").SaveX(ctx)
	order := client.PaymentOrder.Create().SetUserID(user.ID).SetUserEmail(user.Email).SetUserName("payer").
		SetAmount(110).SetBonusAmount(10).SetPayAmount(100).SetFeeRate(0).SetRechargeCode("test-recharge").
		SetOutTradeNo("ORDER123").SetPaymentType(payment.TypeAlipay).SetPaymentTradeNo("").SetOrderType(payment.OrderTypeBalance).
		SetStatus(OrderStatusPending).SetExpiresAt(time.Now().Add(time.Hour)).SetClientIP("127.0.0.1").SetSrcHost("site.example.com").SaveX(ctx)
	svc := &PaymentService{entClient: client, resumeService: NewPaymentResumeService([]byte("test-resume-signing-key"))}
	response, err := svc.invokeProvider(ctx, order, CreateOrderRequest{
		UserID: user.ID, PaymentType: payment.TypeAlipay, OrderType: payment.OrderTypeBalance,
		SrcHost:   "site.example.com",
		ReturnURL: "https://site.example.com/payment/result?trade_status=TRADE_SUCCESS&order_id=999&resume_token=forged&from=checkout#fragment",
	}, &PaymentConfig{}, 100, "100.00", 100, nil, &payment.InstanceSelection{
		InstanceID: "1", ProviderKey: payment.TypeEasyPay,
		Config: map[string]string{
			"pid": "1000", "pkey": "test-key", "apiBase": "https://pay.example.com", "paymentMode": "popup",
			"notifyUrl": "https://site.example.com/api/v1/payment/webhook/easypay", "returnUrl": "https://site.example.com/payment/result",
		},
	})
	require.NoError(t, err)
	checkout, err := url.Parse(response.PayURL)
	require.NoError(t, err)
	returnURL, err := url.Parse(checkout.Query().Get("return_url"))
	require.NoError(t, err)
	require.Equal(t, "", returnURL.Fragment)
	require.Equal(t, url.Values{
		"order_id": {strconv.FormatInt(order.ID, 10)}, "out_trade_no": {"ORDER123"},
		"resume_token": {response.ResumeToken}, "status": {"success"},
	}, returnURL.Query())
	require.NotEmpty(t, response.ResumeToken)
	claims, err := svc.resumeService.ParseToken(response.ResumeToken)
	require.NoError(t, err)
	require.Equal(t, order.ID, claims.OrderID)
	require.Equal(t, user.ID, claims.UserID)
	require.Equal(t, "https://site.example.com/payment/result", claims.CanonicalReturnURL)
	persisted := client.PaymentOrder.GetX(ctx, order.ID)
	require.Equal(t, float64(110), persisted.Amount)
	require.Equal(t, float64(10), persisted.BonusAmount)
	require.Equal(t, OrderStatusPending, persisted.Status)
}
