//go:build unit || !integration

package handler

import (
	"context"
	"encoding/json"
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

type v3HandlerRepo struct {
	reads  int
	window time.Duration
	data   *service.ChannelMonitorV3Data
	err    error
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
func (r *v3HandlerRepo) Read(_ context.Context, _ time.Time, window time.Duration) (*service.ChannelMonitorV3Data, error) {
	r.reads++
	r.window = window
	return r.data, r.err
}

func TestChannelMonitorV3HandlerGlobalStatusForAllReaders(t *testing.T) {
	gin.SetMode(gin.TestMode)
	now := time.Now().UTC()
	repo := &v3HandlerRepo{data: &service.ChannelMonitorV3Data{
		DataThrough: &now,
		Catalog: []service.ChannelMonitorV3Catalog{
			{Platform: "openai", GroupID: 7, GroupName: "Shared"},
			{Platform: "anthropic", GroupID: 88, GroupName: "Exclusive"},
		},
		Current: []service.ChannelMonitorV3Fact{
			{Platform: "openai", GroupID: 7, Model: "gpt", Success: 10, LastRequest: now},
			{Platform: "anthropic", GroupID: 88, Model: "claude", Failures: 10, LastRequest: now},
		},
		Incidents: []service.ChannelMonitorV3Incident{{ID: "other-user-event", Platform: "anthropic", GroupID: 88, Model: "claude", Phase: "ongoing"}},
	}}
	h := NewChannelMonitorV3Handler(service.NewChannelMonitorV3Service(repo, nil))
	var baseline *service.ChannelMonitorV3Snapshot
	for _, tc := range []struct {
		name    string
		userID  int64
		role    string
		support bool
	}{{"ordinary user", 42, "user", false}, {"user without group access", 43, "user", false}, {"administrator", 1, "admin", false}, {"assistance", 1, "admin", true}} {
		t.Run(tc.name, func(t *testing.T) {
			router := gin.New()
			router.Use(func(c *gin.Context) {
				c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: tc.userID})
				c.Set(string(middleware.ContextKeyUserRole), tc.role)
				if tc.support {
					c.Set(middleware.SupportReadTargetKey, middleware.SupportReadTarget{Subject: middleware.AuthSubject{UserID: 99}, Role: "user"})
				}
			})
			router.GET("/snapshot", h.Snapshot)
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/snapshot?range=7d&group_id=999&admin=true", nil))
			require.Equal(t, http.StatusOK, rec.Code)
			var body struct {
				Data *service.ChannelMonitorV3Snapshot
			}
			require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
			require.Len(t, body.Data.Platforms, 2)
			require.Equal(t, "other-user-event", body.Data.Incidents[0].ID)
			require.Equal(t, 1, body.Data.Summary.ActiveEvents)
			require.Equal(t, 7*24*time.Hour, repo.window)
			body.Data.ComputedAt = time.Time{}
			if baseline == nil {
				baseline = body.Data
			} else {
				require.Equal(t, baseline, body.Data)
			}
			for _, sensitive := range []string{"user_id", "api_key_id", "account_id", "actual_cost", "request_id", "error_message"} {
				require.NotContains(t, rec.Body.String(), sensitive)
			}
		})
	}
	require.Equal(t, 4, repo.reads)
	// Platform filtering applies to both the status and its incident history.
	router := gin.New()
	router.Use(func(c *gin.Context) { c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: 42}) })
	router.GET("/snapshot", h.Snapshot)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/snapshot?platform=openai", nil))
	require.Equal(t, http.StatusOK, rec.Code)
	require.NotContains(t, rec.Body.String(), "other-user-event")
	repo.err = errors.New("sensitive SQL credential details")
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/snapshot", nil))
	require.Equal(t, http.StatusInternalServerError, rec.Code)
	require.NotContains(t, rec.Body.String(), "sensitive")
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
