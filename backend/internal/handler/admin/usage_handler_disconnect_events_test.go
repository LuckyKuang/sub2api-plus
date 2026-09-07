//go:build unit

package admin

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type disconnectEventRepoStub struct {
	filter service.ClientDisconnectRiskEventFilter
}

func (r *disconnectEventRepoStub) Begin(context.Context, service.ClientDisconnectRiskBegin) (int64, error) {
	return 0, nil
}
func (r *disconnectEventRepoStub) Finalize(context.Context, service.ClientDisconnectRiskFinalize) (service.ClientDisconnectRiskResult, error) {
	return service.ClientDisconnectRiskResult{}, nil
}
func (r *disconnectEventRepoStub) ClearUser(context.Context, int64) error { return nil }
func (r *disconnectEventRepoStub) ListEvents(_ context.Context, filter service.ClientDisconnectRiskEventFilter) ([]service.ClientDisconnectRiskEvent, int64, error) {
	r.filter = filter
	return []service.ClientDisconnectRiskEvent{{UserID: 7, Generation: 1, Sequence: 1, UsageMissing: true}}, 1, nil
}

func TestUsageHandlerListClientDisconnectEventsValidatesAndForwardsFilters(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := &disconnectEventRepoStub{}
	h := NewUsageHandler(nil, nil, nil, nil)
	h.SetClientDisconnectRiskService(service.NewClientDisconnectRiskService(repo, nil, nil))
	router := gin.New()
	router.GET("/events", h.ListClientDisconnectEvents)

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/events?user_id=7&api_key_id=11&request_id=req-1&session_id=session-1&protocol=openai_responses&outcome=client_disconnected&completion_status=client_disconnected&usage_source=partial&usage_missing=true&enforce=true&auto_banned=false&accepted_from=2026-09-01T00:00:00Z&accepted_to=2026-09-02T00:00:00Z&finalized_from=2026-09-01T00:01:00Z&finalized_to=2026-09-02T00:01:00Z&page=2&page_size=10", nil)
	router.ServeHTTP(recorder, request)

	require.Equal(t, http.StatusOK, recorder.Code)
	require.Equal(t, int64(7), repo.filter.UserID)
	require.Equal(t, int64(11), repo.filter.APIKeyID)
	require.Equal(t, "req-1", repo.filter.RequestID)
	require.Equal(t, "session-1", repo.filter.SessionID)
	require.Equal(t, "openai_responses", repo.filter.Protocol)
	require.Equal(t, "client_disconnected", repo.filter.Outcome)
	require.Equal(t, "client_disconnected", repo.filter.CompletionStatus)
	require.Equal(t, "partial", repo.filter.UsageSource)
	require.NotNil(t, repo.filter.UsageMissing)
	require.True(t, *repo.filter.UsageMissing)
	require.NotNil(t, repo.filter.Enforce)
	require.True(t, *repo.filter.Enforce)
	require.NotNil(t, repo.filter.AutoBanned)
	require.False(t, *repo.filter.AutoBanned)
	require.Equal(t, time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC), *repo.filter.AcceptedFrom)
	require.Equal(t, time.Date(2026, 9, 2, 0, 0, 0, 0, time.UTC), *repo.filter.AcceptedTo)
	require.Equal(t, time.Date(2026, 9, 1, 0, 1, 0, 0, time.UTC), *repo.filter.FinalizedFrom)
	require.Equal(t, time.Date(2026, 9, 2, 0, 1, 0, 0, time.UTC), *repo.filter.FinalizedTo)
	require.Equal(t, 2, repo.filter.Page)
	require.Equal(t, 10, repo.filter.PageSize)
}

func TestUsageHandlerListClientDisconnectEventsRejectsInvertedTimeRange(t *testing.T) {
	h := NewUsageHandler(nil, nil, nil, nil)
	router := gin.New()
	router.GET("/events", h.ListClientDisconnectEvents)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/events?accepted_from=2026-09-02T00:00:00Z&accepted_to=2026-09-01T00:00:00Z", nil))
	require.Equal(t, http.StatusBadRequest, recorder.Code)
}

func TestUsageHandlerListClientDisconnectEventsRejectsUnknownStatus(t *testing.T) {
	h := NewUsageHandler(nil, nil, nil, nil)
	router := gin.New()
	router.GET("/events", h.ListClientDisconnectEvents)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/events?completion_status=anything", nil))
	require.Equal(t, http.StatusBadRequest, recorder.Code)
}
