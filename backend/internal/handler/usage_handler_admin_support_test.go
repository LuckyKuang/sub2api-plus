package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/pkg/usagestats"
	"github.com/LuckyKuang/sub2api-plus/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type adminSupportUsageRepoStub struct {
	service.UsageLogRepository
	filters usagestats.UsageLogFilters
	stats   *usagestats.UsageStats
}

func (s *adminSupportUsageRepoStub) GetStatsWithFilters(_ context.Context, filters usagestats.UsageLogFilters) (*usagestats.UsageStats, error) {
	s.filters = filters
	return s.stats, nil
}

func TestUsageHandlerAdminSupportStatsUsesTargetUserAndRealStats(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := &adminSupportUsageRepoStub{stats: &usagestats.UsageStats{
		TotalRequests:     17,
		TotalTokens:       1234,
		TotalCost:         4.5,
		TotalActualCost:   3.25,
		AverageDurationMs: 890,
	}}
	h := NewUsageHandler(service.NewUsageService(repo, nil, nil, nil), nil, nil, nil)
	router := gin.New()
	router.GET("/api/v1/admin/support/users/:user_id/usage", h.AdminSupportStats)

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/admin/support/users/42/usage?period=week&timezone=UTC", nil)
	router.ServeHTTP(recorder, request)

	require.Equal(t, http.StatusOK, recorder.Code)
	require.Equal(t, int64(42), repo.filters.UserID)
	require.NotNil(t, repo.filters.StartTime)
	require.NotNil(t, repo.filters.EndTime)
	require.True(t, repo.filters.StartTime.Before(*repo.filters.EndTime))
	require.Equal(t, time.Monday, repo.filters.StartTime.Weekday())
	require.Equal(t, 0, repo.filters.StartTime.Hour())
	require.Equal(t, 0, repo.filters.StartTime.Minute())

	var responseBody struct {
		Data adminSupportUsageStats `json:"data"`
	}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &responseBody))
	require.Equal(t, "week", responseBody.Data.Period)
	require.Equal(t, int64(17), responseBody.Data.TotalRequests)
	require.Equal(t, int64(1234), responseBody.Data.TotalTokens)
	require.Equal(t, 4.5, responseBody.Data.TotalCost)
	require.Equal(t, 3.25, responseBody.Data.TotalActualCost)
	require.Equal(t, 890.0, responseBody.Data.AverageDurationMs)
}

func TestUsageHandlerAdminSupportStatsUsesCalendarMonthBoundary(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := &adminSupportUsageRepoStub{stats: &usagestats.UsageStats{}}
	h := NewUsageHandler(service.NewUsageService(repo, nil, nil, nil), nil, nil, nil)
	router := gin.New()
	router.GET("/api/v1/admin/support/users/:user_id/usage", h.AdminSupportStats)

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/admin/support/users/42/usage?period=month&timezone=UTC", nil)
	router.ServeHTTP(recorder, request)

	require.Equal(t, http.StatusOK, recorder.Code)
	require.NotNil(t, repo.filters.StartTime)
	require.Equal(t, 1, repo.filters.StartTime.Day())
	require.Equal(t, 0, repo.filters.StartTime.Hour())
	require.Equal(t, time.UTC, repo.filters.StartTime.Location())
}

func TestUsageHandlerAdminSupportStatsRejectsInvalidPeriod(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := &adminSupportUsageRepoStub{stats: &usagestats.UsageStats{}}
	h := NewUsageHandler(service.NewUsageService(repo, nil, nil, nil), nil, nil, nil)
	router := gin.New()
	router.GET("/api/v1/admin/support/users/:user_id/usage", h.AdminSupportStats)

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/admin/support/users/42/usage?period=year", nil)
	router.ServeHTTP(recorder, request)

	require.Equal(t, http.StatusBadRequest, recorder.Code)
	require.Zero(t, repo.filters.UserID)
}
