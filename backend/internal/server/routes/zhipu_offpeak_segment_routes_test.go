//go:build unit || !integration

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

func TestZhipuOffPeakSegmentRoutesRequireRealAdmin(t *testing.T) {
	gin.SetMode(gin.TestMode)
	auth := service.NewAuthService(nil, nil, nil, nil, &config.Config{JWT: config.JWTConfig{Secret: "offpeak-admin-route-test", ExpireHour: 1}}, nil, nil, nil, nil, nil, nil, nil, nil)
	user := &service.User{ID: 42, Role: service.RoleUser, Status: service.StatusActive}
	users := service.NewUserService(&supportAuthUserRepo{user: user}, nil, nil, nil)
	router := gin.New()
	h := &handler.Handlers{Admin: &handler.AdminHandlers{ZhipuOAuth: admin.NewZhipuOAuthHandler(nil, nil)}}
	group := router.Group("/api/v1/admin", gin.HandlerFunc(middleware.NewAdminAuthMiddleware(auth, users, nil, nil)))
	registerZhipuOAuthRoutes(group, h)
	token, err := auth.GenerateToken(context.Background(), user)
	require.NoError(t, err)
	for _, method := range []string{http.MethodGet, http.MethodPost} {
		path := "/api/v1/admin/zhipu/accounts/501/off-peak/segment"
		if method == http.MethodPost {
			path += "/terminate"
		}
		for _, scenario := range []struct {
			token  string
			status int
		}{{"", http.StatusUnauthorized}, {token, http.StatusForbidden}} {
			recorder := httptest.NewRecorder()
			request := httptest.NewRequest(method, path, strings.NewReader(`{"record_id":"record","generation":"generation"}`))
			request.Header.Set("Content-Type", "application/json")
			if scenario.token != "" {
				request.Header.Set("Authorization", "Bearer "+scenario.token)
			}
			router.ServeHTTP(recorder, request)
			require.Equal(t, scenario.status, recorder.Code, "admin authentication must reject before account lookup or segment control")
		}
	}
}
