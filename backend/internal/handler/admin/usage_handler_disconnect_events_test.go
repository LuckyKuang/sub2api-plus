//go:build unit

package admin

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

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
	request := httptest.NewRequest(http.MethodGet, "/events?user_id=7&api_key_id=11&outcome=client_disconnected&completion_status=client_disconnected&usage_missing=true&auto_banned=false&page=2&page_size=10", nil)
	router.ServeHTTP(recorder, request)

	require.Equal(t, http.StatusOK, recorder.Code)
	require.Equal(t, int64(7), repo.filter.UserID)
	require.Equal(t, int64(11), repo.filter.APIKeyID)
	require.Equal(t, "client_disconnected", repo.filter.Outcome)
	require.Equal(t, "client_disconnected", repo.filter.CompletionStatus)
	require.NotNil(t, repo.filter.UsageMissing)
	require.True(t, *repo.filter.UsageMissing)
	require.NotNil(t, repo.filter.AutoBanned)
	require.False(t, *repo.filter.AutoBanned)
	require.Equal(t, 2, repo.filter.Page)
	require.Equal(t, 10, repo.filter.PageSize)
}

func TestUsageHandlerListClientDisconnectEventsRejectsUnknownStatus(t *testing.T) {
	h := NewUsageHandler(nil, nil, nil, nil)
	router := gin.New()
	router.GET("/events", h.ListClientDisconnectEvents)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/events?completion_status=anything", nil))
	require.Equal(t, http.StatusBadRequest, recorder.Code)
}
