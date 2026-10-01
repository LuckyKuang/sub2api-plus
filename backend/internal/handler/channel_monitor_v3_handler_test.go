//go:build unit || !integration

package handler

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/server/middleware"
	"github.com/LuckyKuang/sub2api-plus/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type v3HandlerGroups struct {
	user int64
	err  error
}

func (g *v3HandlerGroups) GetAvailableGroups(_ context.Context, id int64) ([]service.Group, error) {
	g.user = id
	return []service.Group{{ID: 7}}, g.err
}

type v3HandlerRepo struct {
	ids []int64
	err error
}

func (r *v3HandlerRepo) GetConfig(context.Context) (*service.ChannelMonitorV3Config, error) {
	c := service.DefaultChannelMonitorV3Config()
	return &c, nil
}
func (r *v3HandlerRepo) UpdateConfig(_ context.Context, c service.ChannelMonitorV3Config) (*service.ChannelMonitorV3Config, error) {
	return &c, nil
}
func (r *v3HandlerRepo) Refresh(context.Context, time.Time, service.ChannelMonitorV3Config) error {
	return nil
}
func (r *v3HandlerRepo) Read(_ context.Context, ids []int64, _ time.Time, _ time.Duration) (*service.ChannelMonitorV3Data, error) {
	r.ids = ids
	return &service.ChannelMonitorV3Data{}, r.err
}

func TestChannelMonitorV3HandlerServerDerivesScope(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := &v3HandlerRepo{}
	groups := &v3HandlerGroups{}
	h := &ChannelMonitorV3Handler{service: service.NewChannelMonitorV3Service(repo, nil), groups: groups}
	router := gin.New()
	router.Use(func(c *gin.Context) { c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: 42}) })
	router.GET("/snapshot", h.Snapshot)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/snapshot?range=7d&group_id=999&admin=true", nil))
	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, int64(42), groups.user)
	require.Equal(t, []int64{7}, repo.ids)
	require.NotContains(t, rec.Body.String(), "999")
	repo.err = errors.New("sensitive SQL credential details")
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/snapshot", nil))
	require.Equal(t, http.StatusInternalServerError, rec.Code)
	require.NotContains(t, rec.Body.String(), "sensitive")
	groups.err = errors.New("authorization unavailable")
	repo.ids = nil
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/snapshot", nil))
	require.NotEqual(t, http.StatusOK, rec.Code)
	require.Nil(t, repo.ids)
}
func TestChannelMonitorV3HandlerRejectsUnauthenticatedAndBadRange(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := &ChannelMonitorV3Handler{}
	router := gin.New()
	router.GET("/snapshot", h.Snapshot)
	for _, tc := range []struct {
		path   string
		status int
	}{{"/snapshot", 401}, {"/snapshot?range=90m", 400}} {
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, tc.path, nil))
		require.Equal(t, tc.status, rec.Code)
	}
}
