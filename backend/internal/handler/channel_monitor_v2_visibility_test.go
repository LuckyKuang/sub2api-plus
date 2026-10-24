//go:build unit || !integration

package handler

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/LuckyKuang/sub2api-plus/internal/server/middleware"
	"github.com/LuckyKuang/sub2api-plus/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type channelMonitorV2VisibilityRepo struct {
	service.ChannelMonitorV2Repository
	filter     service.ChannelMonitorV2Filter
	config     service.ChannelMonitorV2Config
	catalogErr error
	updateErr  error
}

func (r *channelMonitorV2VisibilityRepo) GetConfig(context.Context) (*service.ChannelMonitorV2Config, error) {
	return &r.config, nil
}
func (r *channelMonitorV2VisibilityRepo) GetModelCatalog(_ context.Context, filter service.ChannelMonitorV2Filter, _ service.ChannelMonitorV2Config) (*service.ChannelMonitorV2ModelCatalog, error) {
	r.filter = filter
	return &service.ChannelMonitorV2ModelCatalog{Items: []service.ChannelMonitorV2ModelCatalogItem{}}, r.catalogErr
}
func (r *channelMonitorV2VisibilityRepo) UpdateConfig(_ context.Context, cfg service.ChannelMonitorV2Config, _ int) (*service.ChannelMonitorV2Config, error) {
	r.config = cfg
	return &cfg, r.updateErr
}

func TestChannelMonitorV2ModelCatalogQueryAndFailureContracts(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tc := range []struct {
		query  string
		err    error
		status int
	}{
		{"?platform=openai&group_id=7", nil, http.StatusOK},
		{"?group_id=invalid", nil, http.StatusBadRequest},
		{"?range=forever", nil, http.StatusBadRequest},
		{"", service.ErrChannelMonitorV2CatalogTooLarge, http.StatusUnprocessableEntity},
		{"", context.DeadlineExceeded, http.StatusGatewayTimeout},
		{"", errors.New("database failure"), http.StatusInternalServerError},
	} {
		repo := &channelMonitorV2VisibilityRepo{catalogErr: tc.err}
		h := NewChannelMonitorV2Handler(service.NewChannelMonitorV2Service(repo), nil)
		recorder := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(recorder)
		c.Request = httptest.NewRequest(http.MethodGet, "/model-catalog"+tc.query, nil)
		h.ModelCatalog(c)
		require.Equal(t, tc.status, recorder.Code)
		if tc.status == http.StatusOK {
			require.Equal(t, "30d", repo.filter.Range)
			require.Equal(t, []string{"openai"}, repo.filter.Platforms)
			require.Equal(t, []int64{7}, repo.filter.GroupIDs)
		}
	}
}

func TestChannelMonitorV2VisibilityConfigRetainsExplicitEmptySelectionAndVersionConflict(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, updateErr := range []error{nil, service.ErrChannelMonitorV2ConfigConflict} {
		repo := &channelMonitorV2VisibilityRepo{updateErr: updateErr}
		h := NewChannelMonitorV2Handler(service.NewChannelMonitorV2Service(repo), nil)
		recorder := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(recorder)
		c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: 42})
		c.Request = httptest.NewRequest(http.MethodPut, "/config", strings.NewReader(`{"version":3,"platforms":[{"platform":"custom","enabled":true,"models":[],"model_display_mode":"selected","excluded_models":["Hidden"]}]}`))
		c.Request.Header.Set("Content-Type", "application/json")
		h.UpdateConfig(c)
		if updateErr != nil {
			require.Equal(t, http.StatusConflict, recorder.Code)
		} else {
			require.Equal(t, http.StatusOK, recorder.Code)
			require.Contains(t, recorder.Body.String(), `"model_display_mode":"selected"`)
			require.Contains(t, recorder.Body.String(), `"excluded_models":["Hidden"]`)
		}
		require.Equal(t, 3, repo.config.Version)
		require.Equal(t, service.ChannelMonitorV2DisplaySelected, repo.config.Platforms[0].ModelDisplayMode)
		require.Empty(t, repo.config.Platforms[0].Models)
	}
}
