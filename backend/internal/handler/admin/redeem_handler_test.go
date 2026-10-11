//go:build unit || !integration

package admin

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	infraerrors "github.com/LuckyKuang/sub2api-plus/internal/pkg/errors"
	"github.com/LuckyKuang/sub2api-plus/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type createAndRedeemValidationRepo struct{ service.RedeemCodeRepository }

func (*createAndRedeemValidationRepo) GetByCode(context.Context, string) (*service.RedeemCode, error) {
	return nil, infraerrors.ServiceUnavailable("TEST_REDEEM_LOOKUP_UNAVAILABLE", "injected redeem lookup failure")
}

// A controlled repository failure identifies requests that actually reach the
// service boundary after validation. A panic must fail the test, never count as
// successful validation. These tests do not claim fulfillment success.
func newCreateAndRedeemHandler() *RedeemHandler {
	return &RedeemHandler{
		adminService:  newStubAdminService(),
		redeemService: service.NewRedeemService(&createAndRedeemValidationRepo{}, nil, nil, nil, nil, nil, nil, nil),
	}
}

func postCreateAndRedeemValidation(t *testing.T, handler *RedeemHandler, body any) int {
	t.Helper()
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)

	jsonBytes, err := json.Marshal(body)
	require.NoError(t, err)
	c.Request, _ = http.NewRequest(http.MethodPost, "/api/v1/admin/redeem-codes/create-and-redeem", bytes.NewReader(jsonBytes))
	c.Request.Header.Set("Content-Type", "application/json")

	handler.CreateAndRedeem(c)
	if w.Code == http.StatusServiceUnavailable {
		require.Contains(t, w.Body.String(), "TEST_REDEEM_LOOKUP_UNAVAILABLE")
	}
	return w.Code
}

func TestCreateAndRedeem_TypeDefaultsToBalance(t *testing.T) {
	// 不传 type 字段时应默认 balance，不触发 subscription 校验。
	// 合法输入必须到达可识别的仓储失败边界。
	h := newCreateAndRedeemHandler()
	code := postCreateAndRedeemValidation(t, h, map[string]any{
		"code":    "test-balance-default",
		"value":   10.0,
		"user_id": 1,
	})

	assert.Equal(t, http.StatusServiceUnavailable, code,
		"omitting type should default to balance and pass validation")
}

func TestCreateAndRedeem_SubscriptionRequiresGroupID(t *testing.T) {
	h := newCreateAndRedeemHandler()
	code := postCreateAndRedeemValidation(t, h, map[string]any{
		"code":          "test-sub-no-group",
		"type":          "subscription",
		"value":         29.9,
		"user_id":       1,
		"validity_days": 30,
		// group_id 缺失
	})

	assert.Equal(t, http.StatusBadRequest, code)
}

func TestCreateAndRedeem_SubscriptionRequiresNonZeroValidityDays(t *testing.T) {
	groupID := int64(5)
	h := newCreateAndRedeemHandler()

	// zero should be rejected
	t.Run("zero", func(t *testing.T) {
		code := postCreateAndRedeemValidation(t, h, map[string]any{
			"code":          "test-sub-bad-days-zero",
			"type":          "subscription",
			"value":         29.9,
			"user_id":       1,
			"group_id":      groupID,
			"validity_days": 0,
		})

		assert.Equal(t, http.StatusBadRequest, code)
	})

	// negative should pass validation (used for refund/reduction)
	t.Run("negative_passes_validation", func(t *testing.T) {
		code := postCreateAndRedeemValidation(t, h, map[string]any{
			"code":          "test-sub-negative-days",
			"type":          "subscription",
			"value":         29.9,
			"user_id":       1,
			"group_id":      groupID,
			"validity_days": -7,
		})

		assert.Equal(t, http.StatusServiceUnavailable, code,
			"negative validity_days should pass validation for refund")
	})
}

func TestCreateAndRedeem_SubscriptionValidParamsPassValidation(t *testing.T) {
	groupID := int64(5)
	h := newCreateAndRedeemHandler()
	code := postCreateAndRedeemValidation(t, h, map[string]any{
		"code":          "test-sub-valid",
		"type":          "subscription",
		"value":         29.9,
		"user_id":       1,
		"group_id":      groupID,
		"validity_days": 31,
	})

	assert.Equal(t, http.StatusServiceUnavailable, code,
		"valid subscription params should pass validation")
}

func TestCreateAndRedeem_BalanceIgnoresSubscriptionFields(t *testing.T) {
	h := newCreateAndRedeemHandler()
	// balance 类型不传 group_id 和 validity_days，不应报 400
	code := postCreateAndRedeemValidation(t, h, map[string]any{
		"code":    "test-balance-no-extras",
		"type":    "balance",
		"value":   50.0,
		"user_id": 1,
	})

	assert.Equal(t, http.StatusServiceUnavailable, code,
		"balance type should not require group_id or validity_days")
}

// Subscription order price/entitlement come from the selected plan, so callers
// must be able to omit value/group/duration (ADMIN_PAYMENT_INTEGRATION_API.md).
func TestCreateAndRedeemSubscriptionOrderRequestDoesNotRequireValue(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/", bytes.NewBufferString(`{"code":"sub-order-code","type":"subscription","generate_order":true,"order_plan_id":7,"user_id":123}`))
	c.Request.Header.Set("Content-Type", "application/json")
	var req CreateAndRedeemCodeRequest
	require.NoError(t, c.ShouldBindJSON(&req))
	require.True(t, req.GenerateOrder)
	require.Equal(t, int64(7), *req.OrderPlanID)
	require.Zero(t, req.Value)
	require.Nil(t, req.GroupID)
	require.Zero(t, req.ValidityDays)
	require.Equal(t, http.StatusServiceUnavailable, postCreateAndRedeemValidation(t, newCreateAndRedeemHandler(), map[string]any{
		"code": "sub-order-code", "type": "subscription", "generate_order": true, "order_plan_id": 7, "user_id": 123,
	}), "omitting plan-derived fields must reach the service boundary")
}

func TestResolveRedeemCodeExpiresAt_FromDays(t *testing.T) {
	days := 3
	expiresAt, err := resolveRedeemCodeExpiresAt(nil, &days)
	require.NoError(t, err)
	require.NotNil(t, expiresAt)
	require.WithinDuration(t, time.Now().UTC().AddDate(0, 0, days), *expiresAt, 2*time.Second)
}

func TestResolveRedeemCodeExpiresAt_RejectsPastAbsoluteTime(t *testing.T) {
	past := time.Now().UTC().Add(-time.Minute)
	expiresAt, err := resolveRedeemCodeExpiresAt(&past, nil)
	require.Error(t, err)
	require.Nil(t, expiresAt)
}

func TestResolveRedeemCodeExpiresAt_RejectsNonPositiveDays(t *testing.T) {
	days := 0
	expiresAt, err := resolveRedeemCodeExpiresAt(nil, &days)
	require.Error(t, err)
	require.Nil(t, expiresAt)
}

func TestResolveRedeemCodeExpiresAt_RejectsConflictingInputs(t *testing.T) {
	future := time.Now().UTC().Add(time.Hour)
	days := 3
	expiresAt, err := resolveRedeemCodeExpiresAt(&future, &days)
	require.Error(t, err)
	require.Nil(t, expiresAt)
}
