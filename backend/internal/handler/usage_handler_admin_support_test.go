//go:build unit || !integration

package handler

import (
	"bytes"
	"context"
	"github.com/LuckyKuang/sub2api-plus/internal/pkg/usagestats"
	"github.com/LuckyKuang/sub2api-plus/internal/server/middleware"
	"github.com/LuckyKuang/sub2api-plus/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

type supportBatchKeyRepo struct {
	service.APIKeyRepository
	userID int64
	ids    []int64
}

func (r *supportBatchKeyRepo) VerifyOwnership(_ context.Context, userID int64, ids []int64) ([]int64, error) {
	r.userID, r.ids = userID, ids
	return []int64{7}, nil
}

type supportBatchUsageRepo struct {
	service.UsageLogRepository
	ids []int64
}

func (r *supportBatchUsageRepo) GetBatchAPIKeyUsageStats(_ context.Context, ids []int64, _, _ time.Time) (map[int64]*usagestats.BatchAPIKeyUsageStats, error) {
	r.ids = ids
	return map[int64]*usagestats.BatchAPIKeyUsageStats{7: {}}, nil
}

func TestSupportBatchUsageGETMatchesUserPOSTAndChecksTargetOwnership(t *testing.T) {
	gin.SetMode(gin.TestMode)
	keys := &supportBatchKeyRepo{}
	usage := &supportBatchUsageRepo{}
	h := NewUsageHandler(service.NewUsageService(usage, nil, nil, nil), service.NewAPIKeyService(keys, nil, nil, nil, nil, nil, nil), nil, nil)
	user, support := httptest.NewRecorder(), httptest.NewRecorder()
	c := supportParityContext(user, "/usage/dashboard/api-keys-usage", false)
	c.Request = httptest.NewRequest(http.MethodPost, "/usage/dashboard/api-keys-usage", bytes.NewBufferString(`{"api_key_ids":[7,8]}`))
	c.Request.Header.Set("Content-Type", "application/json")
	h.DashboardAPIKeysUsage(c)
	c = supportParityContext(support, "/usage/dashboard/api-keys-usage?api_key_ids=7&api_key_ids=8", true)
	h.DashboardAPIKeysUsage(c)
	require.Equal(t, http.StatusOK, support.Code)
	require.Equal(t, int64(42), keys.userID)
	require.Equal(t, []int64{7, 8}, keys.ids)
	require.Equal(t, []int64{7}, usage.ids)
	require.JSONEq(t, user.Body.String(), support.Body.String())
}

type adminSupportUsageRepoStub struct {
	service.UsageLogRepository
	filters usagestats.UsageLogFilters
}

func (s *adminSupportUsageRepoStub) GetStatsWithFilters(_ context.Context, filters usagestats.UsageLogFilters) (*usagestats.UsageStats, error) {
	s.filters = filters
	return &usagestats.UsageStats{TotalRequests: 17, TotalTokens: 1234, TotalCost: 4.5, TotalActualCost: 3.25, AverageDurationMs: 890}, nil
}

func TestUserSupportUsageMatchesUserResponseAndScopesTarget(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := &adminSupportUsageRepoStub{}
	h := NewUsageHandler(service.NewUsageService(repo, nil, nil, nil), nil, nil, nil)
	invoke := func(support bool) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(rec)
		c.Request = httptest.NewRequest(http.MethodGet, "/usage/stats?start_date=2026-09-01&end_date=2026-09-30&timezone=UTC", nil)
		actor := int64(42)
		if support {
			actor = 1
		}
		c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: actor})
		if support {
			c.Set(string(middleware.ContextKeyUserRole), "admin")
			c.Set(middleware.SupportReadTargetKey, middleware.SupportReadTarget{Subject: middleware.AuthSubject{UserID: 42}, Role: "user"})
		}
		h.Stats(c)
		require.Equal(t, int64(42), repo.filters.UserID)
		return rec
	}
	user := invoke(false)
	support := invoke(true)
	require.Equal(t, http.StatusOK, user.Code)
	require.Equal(t, user.Code, support.Code)
	require.JSONEq(t, user.Body.String(), support.Body.String())
}
