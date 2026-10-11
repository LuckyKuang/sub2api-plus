//go:build unit

package handler

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	dbent "github.com/LuckyKuang/sub2api-plus/ent"
	"github.com/LuckyKuang/sub2api-plus/ent/enttest"
	"github.com/LuckyKuang/sub2api-plus/internal/server/middleware"
	"github.com/LuckyKuang/sub2api-plus/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"
)

// PAYMENT.md: authenticated order responses show source/type/frozen plan name,
// but never expose raw codes, provider secrets or admin fields. Verify the HTTP
// boundary with a real database instead of only inspecting a DTO helper.
func TestRedeemOrderHTTPResponsePreservesBusinessFieldsAndPrivacy(t *testing.T) {
	ctx := context.Background()
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "orders.db"))
	require.NoError(t, err)
	db.SetMaxOpenConns(1)
	_, err = db.Exec("PRAGMA foreign_keys = ON")
	require.NoError(t, err)
	client := enttest.NewClient(t, enttest.WithOptions(dbent.Driver(entsql.OpenDB(dialect.SQLite, db))))
	t.Cleanup(func() { _ = client.Close() })
	user, err := client.User.Create().SetEmail("owner@example.com").SetPasswordHash("test").Save(ctx)
	require.NoError(t, err)
	other, err := client.User.Create().SetEmail("other@example.com").SetPasswordHash("test").Save(ctx)
	require.NoError(t, err)
	order, err := client.PaymentOrder.Create().SetUserID(user.ID).SetUserEmail(user.Email).SetUserName("internal-admin-name").
		SetAmount(25).SetPayAmount(25).SetCurrency("CNY").SetOrderSource("redeem").SetOrderType("subscription").
		SetPlanName("Frozen monthly plan").SetRechargeCode("private-raw-code").SetOutTradeNo("public-order-number").
		SetPaymentType("redeem").SetPaymentTradeNo("").SetStatus("COMPLETED").SetExpiresAt(time.Now()).
		SetClientIP("192.0.2.9").SetSrcHost("internal.example").SetProviderSnapshot(map[string]any{"secret": "private-provider-secret"}).Save(ctx)
	require.NoError(t, err)
	h := NewPaymentHandler(service.NewPaymentService(client, nil, nil, nil, nil, nil, nil, nil, nil), nil)
	for _, list := range []bool{false, true} {
		recorder := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(recorder)
		c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: user.ID})
		c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/payment/orders/my?order_source=redeem&order_type=subscription", nil)
		if list {
			h.GetMyOrders(c)
		} else {
			c.Params = gin.Params{{Key: "id", Value: fmt.Sprint(order.ID)}}
			h.GetOrder(c)
		}
		require.Equal(t, http.StatusOK, recorder.Code)
		var response struct {
			Data json.RawMessage `json:"data"`
		}
		require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &response))
		var result map[string]any
		if list {
			var page struct {
				Items []map[string]any `json:"items"`
				Total int              `json:"total"`
			}
			require.NoError(t, json.Unmarshal(response.Data, &page))
			require.Equal(t, 1, page.Total)
			require.Len(t, page.Items, 1)
			result = page.Items[0]
		} else {
			require.NoError(t, json.Unmarshal(response.Data, &result))
		}
		require.Equal(t, "redeem", result["order_source"])
		require.Equal(t, "subscription", result["order_type"])
		require.Equal(t, "Frozen monthly plan", result["plan_name"])
		require.Equal(t, 25.0, result["pay_amount"])
		require.Equal(t, "CNY", result["currency"])
		for _, key := range []string{"recharge_code", "redeem_code_id", "provider_snapshot", "user_email", "user_name", "client_ip", "src_host"} {
			require.NotContains(t, result, key)
		}
		for _, secret := range []string{"private-raw-code", "private-provider-secret", "internal-admin-name"} {
			require.NotContains(t, recorder.Body.String(), secret)
		}
	}
	// A new source must not weaken existing order ownership.
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: other.ID})
	c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/payment/orders/1", nil)
	c.Params = gin.Params{{Key: "id", Value: fmt.Sprint(order.ID)}}
	h.GetOrder(c)
	require.Equal(t, http.StatusForbidden, recorder.Code)
	require.NotContains(t, recorder.Body.String(), "Frozen monthly plan")
}
