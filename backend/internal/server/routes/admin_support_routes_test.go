//go:build unit || !integration

package routes

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/LuckyKuang/sub2api-plus/internal/config"
	"github.com/LuckyKuang/sub2api-plus/internal/handler"
	"github.com/LuckyKuang/sub2api-plus/internal/handler/admin"
	"github.com/LuckyKuang/sub2api-plus/internal/server/middleware"
	"github.com/LuckyKuang/sub2api-plus/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type supportAuthUserRepo struct {
	service.UserRepository
	user *service.User
}

func (r *supportAuthUserRepo) GetByID(context.Context, int64) (*service.User, error) {
	return r.user, nil
}

func (r *supportAuthUserRepo) GetUserAvatar(context.Context, int64) (*service.UserAvatar, error) {
	return nil, nil
}

func TestAdminSupportRejectsUnauthenticatedAndOrdinaryUserBeforeTargetRead(t *testing.T) {
	gin.SetMode(gin.TestMode)
	auth := service.NewAuthService(nil, nil, nil, nil, &config.Config{JWT: config.JWTConfig{Secret: "support-test-secret", ExpireHour: 1}}, nil, nil, nil, nil, nil, nil, nil, nil)
	user := &service.User{ID: 42, Role: service.RoleUser, Status: service.StatusActive}
	users := service.NewUserService(&supportAuthUserRepo{user: user}, nil, nil, nil)
	router := gin.New()
	h := &handler.Handlers{Admin: &handler.AdminHandlers{User: admin.NewUserHandler(nil, nil, nil, nil, nil, nil, nil)}}
	group := router.Group("/api/v1/admin", gin.HandlerFunc(middleware.NewAdminAuthMiddleware(auth, users, nil, nil)))
	registerAdminSupportRoutes(group, h, nil)
	token, err := auth.GenerateToken(context.Background(), user)
	require.NoError(t, err)
	for _, test := range []struct {
		token  string
		status int
	}{{"", http.StatusUnauthorized}, {token, http.StatusForbidden}} {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/support/users/43/keys", nil)
		if test.token != "" {
			req.Header.Set("Authorization", "Bearer "+test.token)
		}
		router.ServeHTTP(rec, req)
		require.Equal(t, test.status, rec.Code)
	}
}

func TestAdminSupportRegistersSharedReadsAndNoMutations(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	h := &handler.Handlers{Admin: &handler.AdminHandlers{User: admin.NewUserHandler(nil, nil, nil, nil, nil, nil, nil)}}
	registerAdminSupportRoutes(r.Group("/api/v1/admin"), h, nil)
	registered := map[string]bool{}
	for _, route := range r.Routes() {
		require.Equal(t, http.MethodGet, route.Method)
		registered[route.Path] = true
	}
	for _, path := range []string{"/user/profile", "/keys", "/groups/available", "/usage/dashboard/trend", "/usage/errors/:id", "/channels/available", "/channel-monitor-v2/snapshot", "/channel-monitor-v3/snapshot", "/user/totp/status", "/user/passkeys", "/images/tasks/:task_id/download", "/images/batches/:id/items/:custom_id/content", "/payment/orders/my", "/redeem/history", "/user/aff"} {
		require.True(t, registered["/api/v1/admin/support/users/:user_id"+path], path)
	}
	for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete} {
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, httptest.NewRequest(method, "/api/v1/admin/support/users/42/keys", nil))
		require.Equal(t, http.StatusNotFound, rec.Code)
	}
	// Target validation runs before a handler can load any user data.
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/admin/support/users/invalid/keys", nil))
	require.Equal(t, http.StatusBadRequest, rec.Code)
}
