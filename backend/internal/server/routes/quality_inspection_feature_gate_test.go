//go:build unit || !integration

package routes

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/LuckyKuang/sub2api-plus/internal/config"
	"github.com/LuckyKuang/sub2api-plus/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func qualityInspectionSettings(values map[string]string) *service.SettingService {
	return service.NewSettingService(&channelMonitorRouteSettingRepoStub{values: values}, &config.Config{})
}

func TestQualityInspectionAdminFeatureGuard(t *testing.T) {
	cases := []struct {
		name   string
		svc    *service.SettingService
		status int
	}{
		{"nil settings blocks", nil, http.StatusForbidden},
		{"disabled blocks", qualityInspectionSettings(map[string]string{
			service.SettingKeyModelQualityInspectionEnabled: "false",
		}), http.StatusForbidden},
		{"missing key blocks", qualityInspectionSettings(map[string]string{}), http.StatusForbidden},
		{"enabled passes", qualityInspectionSettings(map[string]string{
			service.SettingKeyModelQualityInspectionEnabled: "true",
		}), http.StatusOK},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gin.SetMode(gin.TestMode)
			router := gin.New()
			router.Use(qualityInspectionAdminFeatureGuard(tc.svc))
			router.GET("/probe", func(c *gin.Context) { c.Status(http.StatusOK) })

			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/probe", nil))
			require.Equal(t, tc.status, recorder.Code)
		})
	}
}
