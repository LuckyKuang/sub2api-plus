//go:build unit

package handler

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	dbent "github.com/LuckyKuang/sub2api-plus/ent"
	"github.com/LuckyKuang/sub2api-plus/ent/enttest"
	"github.com/LuckyKuang/sub2api-plus/internal/payment"
	"github.com/LuckyKuang/sub2api-plus/internal/payment/provider"
	"github.com/LuckyKuang/sub2api-plus/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"
)

// Exercise the actual HTTP handler, persisted provider lookup, and EasyPay verifier.
// A signed checkout URL must never reach fulfillment, for either order type or verb.
func TestEasyPayWebhookRejectsCheckoutSignatureBeforeFinancialWrites(t *testing.T) {
	for _, orderType := range []string{payment.OrderTypeBalance, payment.OrderTypeSubscription} {
		for _, method := range []string{http.MethodGet, http.MethodPost} {
			t.Run(orderType+"/"+method, func(t *testing.T) {
				ctx := context.Background()
				db, err := sql.Open("sqlite", "file:"+url.QueryEscape(t.Name())+"?mode=memory&cache=shared")
				require.NoError(t, err)
				_, err = db.Exec("PRAGMA foreign_keys = ON")
				require.NoError(t, err)
				client := enttest.NewClient(t, enttest.WithOptions(dbent.Driver(entsql.OpenDB(dialect.SQLite, db))))
				t.Cleanup(func() { _ = client.Close() })
				config := map[string]string{
					"pid": "1000", "pkey": "test-merchant-key", "apiBase": "https://pay.example.com",
					"notifyUrl": "https://site.example.com/api/v1/payment/webhook/easypay", "paymentMode": "popup",
					"returnUrl": "https://site.example.com/payment/result",
				}
				key := []byte("0123456789abcdef0123456789abcdef")
				rawConfig, err := json.Marshal(config)
				require.NoError(t, err)
				encrypted, err := payment.Encrypt(string(rawConfig), key)
				require.NoError(t, err)
				inst := client.PaymentProviderInstance.Create().SetProviderKey(payment.TypeEasyPay).
					SetName("test").SetConfig(encrypted).SetSupportedTypes("alipay").SetPaymentMode("popup").SetEnabled(true).SaveX(ctx)
				user := client.User.Create().SetEmail("payer@example.com").SetPasswordHash("hash").SetBalance(12).SaveX(ctx)
				order := client.PaymentOrder.Create().SetUserID(user.ID).SetUserEmail(user.Email).SetUserName("payer").
					SetAmount(650).SetPayAmount(650).SetFeeRate(0).SetRechargeCode("test-recharge").SetOutTradeNo("ORDER123").
					SetPaymentType(payment.TypeAlipay).SetPaymentTradeNo("").SetOrderType(orderType).SetStatus(service.OrderStatusPending).
					SetExpiresAt(time.Now().Add(time.Hour)).SetClientIP("127.0.0.1").SetSrcHost("site.example.com").
					SetProviderKey(payment.TypeEasyPay).SetProviderInstanceID(strconv.FormatInt(inst.ID, 10)).SaveX(ctx)
				prov, err := provider.NewEasyPay(strconv.FormatInt(inst.ID, 10), config)
				require.NoError(t, err)
				prefix := "https://site.example.com/payment/result?order_id=99&status=success"
				checkout, err := prov.CreatePayment(ctx, payment.CreatePaymentRequest{
					OrderID: order.OutTradeNo, PaymentType: payment.TypeAlipay, Amount: "650.00", Subject: "balance recharge",
					ReturnURL: prefix + "&trade_status=TRADE_SUCCESS",
				})
				require.NoError(t, err)
				payURL, err := url.Parse(checkout.PayURL)
				require.NoError(t, err)
				forged := payURL.Query()
				forged.Set("return_url", prefix)
				forged.Set("trade_status", "TRADE_SUCCESS")

				// Any mutation is a failure, including order state, credits, subscriptions or audit fulfillment.
				writes := 0
				client.Use(func(next dbent.Mutator) dbent.Mutator {
					return dbent.MutateFunc(func(ctx context.Context, m dbent.Mutation) (dbent.Value, error) {
						writes++
						return next.Mutate(ctx, m)
					})
				})
				registry := payment.NewRegistry()
				svc := service.NewPaymentService(client, registry, payment.NewDefaultLoadBalancer(client, key), nil, nil, nil, nil, nil, nil)
				handler := NewPaymentWebhookHandler(svc, registry)
				for _, raw := range []string{payURL.RawQuery, forged.Encode()} {
					recorder := httptest.NewRecorder()
					c, _ := gin.CreateTestContext(recorder)
					endpoint := "/api/v1/payment/webhook/easypay"
					if method == http.MethodGet {
						endpoint += "?" + raw
					}
					c.Request = httptest.NewRequest(method, endpoint, strings.NewReader(raw))
					c.Request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
					handler.EasyPayNotify(c)
					require.Equal(t, http.StatusBadRequest, recorder.Code)
					require.Equal(t, "verify failed", recorder.Body.String())
				}
				require.Zero(t, writes, "rejected payment must not enter fulfillment")
				require.Equal(t, service.OrderStatusPending, client.PaymentOrder.GetX(ctx, order.ID).Status)
				require.Equal(t, float64(12), client.User.GetX(ctx, user.ID).Balance)
				require.Zero(t, client.UserSubscription.Query().CountX(ctx))
				require.Zero(t, client.RedeemCode.Query().CountX(ctx))
			})
		}
	}
}
