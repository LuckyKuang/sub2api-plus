package routes

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/LuckyKuang/sub2api-plus/internal/handler"
	adminhandler "github.com/LuckyKuang/sub2api-plus/internal/handler/admin"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestRetiredGatewayBillingRouteReturnsOrdinaryNotFound(t *testing.T) {
	for _, tc := range []struct {
		name          string
		authorization string
	}{
		{name: "unauthenticated"},
		{name: "authenticated", authorization: "Bearer test-api-key"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			router := newGatewayRoutesTestRouter()
			req := httptest.NewRequest(http.MethodGet, "/v1/sub2api/billing", nil)
			if tc.authorization != "" {
				req.Header.Set("Authorization", tc.authorization)
			}
			response := httptest.NewRecorder()

			router.ServeHTTP(response, req)

			require.Equal(t, http.StatusNotFound, response.Code)
			body := strings.ToLower(response.Body.String())
			require.NotContains(t, body, "sub2api.key_billing")
			require.NotContains(t, body, "migration")
		})
	}
}

func TestRetiredAdminBillingProbeRoutesReturnOrdinaryNotFound(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	admin := router.Group("/api/v1/admin")
	registerAccountRoutes(admin, &handler.Handlers{Admin: &handler.AdminHandlers{
		Account:     adminhandler.NewAccountHandler(nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil),
		OAuth:       &adminhandler.OAuthHandler{},
		OpenAIOAuth: &adminhandler.OpenAIOAuthHandler{},
	}}, nil)

	for _, tc := range []struct {
		method string
		path   string
	}{
		{http.MethodGet, "/api/v1/admin/accounts/upstream-billing-probe/settings"},
		{http.MethodPut, "/api/v1/admin/accounts/upstream-billing-probe/settings"},
		{http.MethodPost, "/api/v1/admin/accounts/upstream-billing-probe/batch"},
		{http.MethodPut, "/api/v1/admin/accounts/42/upstream-billing-probe"},
		{http.MethodPost, "/api/v1/admin/accounts/42/upstream-billing-probe"},
	} {
		t.Run(tc.method+" "+tc.path, func(t *testing.T) {
			response := httptest.NewRecorder()
			router.ServeHTTP(response, httptest.NewRequest(tc.method, tc.path, nil))

			require.Equal(t, http.StatusNotFound, response.Code)
			body := strings.ToLower(response.Body.String())
			require.NotContains(t, body, "upstream-billing-probe")
			require.NotContains(t, body, "migration")
		})
	}
}
