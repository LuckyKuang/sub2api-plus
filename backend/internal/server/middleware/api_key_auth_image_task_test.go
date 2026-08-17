//go:build unit

package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/config"
	"github.com/LuckyKuang/sub2api-plus/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestIsAsyncImageTaskManagement(t *testing.T) {
	for _, tc := range []struct {
		method string
		path   string
		want   bool
	}{
		{http.MethodGet, "/v1/images/tasks", true},
		{http.MethodGet, "/images/tasks", true},
		{http.MethodGet, "/v1/images/tasks/imgtask_123", true},
		{http.MethodGet, "/images/tasks/imgtask_123/download", true},
		{http.MethodDelete, "/v1/images/tasks/imgtask_123", true},
		{http.MethodDelete, "/images/tasks/imgtask_123", true},
		{http.MethodDelete, "/v1/images/tasks", false},
		{http.MethodDelete, "/v1/images/tasks/imgtask_123/download", false},
		{http.MethodPost, "/v1/images/tasks/imgtask_123", false},
		{http.MethodGet, "/v1/images/tasks-extra", false},
		{http.MethodGet, "/v1/images/generations", false},
	} {
		require.Equal(t, tc.want, isAsyncImageTaskManagement(tc.method, tc.path), "%s %s", tc.method, tc.path)
	}
}

func TestAPIKeyAuthTaskManagementSkipsBillingButKeepsAuthentication(t *testing.T) {
	gin.SetMode(gin.TestMode)
	group := &service.Group{
		ID:               42,
		Status:           service.StatusActive,
		Hydrated:         true,
		SubscriptionType: service.SubscriptionTypeSubscription,
	}
	user := &service.User{ID: 7, Role: service.RoleUser, Status: service.StatusActive, Balance: 0}
	expiredAt := time.Now().Add(-time.Hour)
	apiKey := &service.APIKey{
		ID:        100,
		UserID:    user.ID,
		Key:       "task-management-auth-only",
		Status:    service.StatusAPIKeyQuotaExhausted,
		User:      user,
		GroupID:   &group.ID,
		Group:     group,
		Quota:     1,
		QuotaUsed: 1,
		ExpiresAt: &expiredAt,
	}
	apiKeyRepo := &stubApiKeyRepo{
		getByKey: func(context.Context, string) (*service.APIKey, error) {
			clone := *apiKey
			return &clone, nil
		},
		updateLastUsed: func(context.Context, int64, time.Time) error { return nil },
	}
	cfg := &config.Config{RunMode: config.RunModeStandard}
	router := newAuthTestRouter(service.NewAPIKeyService(apiKeyRepo, nil, nil, nil, nil, nil, cfg), nil, cfg)
	ok := func(c *gin.Context) { c.Status(http.StatusNoContent) }
	router.GET("/v1/images/tasks", ok)
	router.DELETE("/v1/images/tasks/:task_id", ok)

	for _, tc := range []struct {
		method string
		path   string
	}{
		{http.MethodGet, "/v1/images/tasks"},
		{http.MethodDelete, "/v1/images/tasks/imgtask_failed"},
	} {
		withKey := httptest.NewRequest(tc.method, tc.path, nil)
		withKey.Header.Set("x-api-key", apiKey.Key)
		withKeyWriter := httptest.NewRecorder()
		router.ServeHTTP(withKeyWriter, withKey)
		require.Equal(t, http.StatusNoContent, withKeyWriter.Code, "%s %s", tc.method, tc.path)

		withoutKeyWriter := httptest.NewRecorder()
		router.ServeHTTP(withoutKeyWriter, httptest.NewRequest(tc.method, tc.path, nil))
		require.Equal(t, http.StatusUnauthorized, withoutKeyWriter.Code, "%s %s", tc.method, tc.path)
	}
}
