//go:build unit

package routes

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/LuckyKuang/sub2api-plus/internal/config"
	"github.com/LuckyKuang/sub2api-plus/internal/handler"
	"github.com/LuckyKuang/sub2api-plus/internal/handler/admin"
	"github.com/LuckyKuang/sub2api-plus/internal/server/middleware"
	"github.com/LuckyKuang/sub2api-plus/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// Admin-only means rejected before appearance storage is touched, even when an
// ordinary user has a valid JWT. Use the real middleware and registered routes.
func TestConsoleAppearanceRoutesRequireRealAdmin(t *testing.T) {
	gin.SetMode(gin.TestMode)
	auth := service.NewAuthService(nil, nil, nil, nil, &config.Config{JWT: config.JWTConfig{Secret: "appearance-test", ExpireHour: 1}}, nil, nil, nil, nil, nil, nil, nil, nil)
	user := &service.User{ID: 42, Role: service.RoleUser, Status: service.StatusActive}
	users := service.NewUserService(&supportAuthUserRepo{user: user}, nil, nil, nil)
	router := gin.New()
	h := &handler.Handlers{Admin: &handler.AdminHandlers{Setting: admin.NewSettingHandler(nil, nil, nil, nil, nil, nil, nil)}}
	group := router.Group("/api/v1/admin", gin.HandlerFunc(middleware.NewAdminAuthMiddleware(auth, users, nil, nil)))
	registerSettingsRoutes(group, h, middleware.StepUpAuthMiddleware(func(c *gin.Context) { c.Next() }))
	token, err := auth.GenerateToken(context.Background(), user)
	require.NoError(t, err)
	for _, method := range []string{http.MethodGet, http.MethodPut} {
		for _, tc := range []struct {
			token  string
			status int
		}{{"", 401}, {token, 403}} {
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(method, "/api/v1/admin/settings/appearance", strings.NewReader(`{"light_mask":30,"dark_mask":48}`))
			if tc.token != "" {
				req.Header.Set("Authorization", "Bearer "+tc.token)
			}
			router.ServeHTTP(rec, req)
			require.Equal(t, tc.status, rec.Code)
		}
	}
}
