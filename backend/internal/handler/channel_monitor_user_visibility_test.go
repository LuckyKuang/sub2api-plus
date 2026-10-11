//go:build unit || !integration

package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/LuckyKuang/sub2api-plus/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type monitorVisibilitySettings struct {
	service.SettingRepository
	values map[string]string
}

func (s monitorVisibilitySettings) GetMultiple(context.Context, []string) (map[string]string, error) {
	return s.values, nil
}

// CHANNEL_MONITOR.md: extra V1 probes affect V3 status, but must not expose
// standalone V1 lists or details when the selected user-visible mode is V3.
func TestChannelMonitorV3ExtraProbesKeepUserV1Hidden(t *testing.T) {
	gin.SetMode(gin.TestMode)
	settings := monitorVisibilitySettings{values: map[string]string{
		service.SettingKeyChannelMonitorEnabled:   "true",
		service.SettingKeyChannelMonitorMode:      "v3",
		service.SettingKeyChannelMonitorV1Enabled: "true",
	}}
	h := NewChannelMonitorUserHandler(nil, service.NewSettingService(settings, nil))
	for _, tc := range []struct {
		path    string
		handler gin.HandlerFunc
		status  int
	}{{"/", h.List, http.StatusOK}, {"/:id/status", h.GetStatus, http.StatusNotFound}} {
		router := gin.New()
		router.GET(tc.path, tc.handler)
		rec := httptest.NewRecorder()
		path := "/"
		if tc.status == http.StatusNotFound {
			path = "/1/status"
		}
		router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		require.Equal(t, tc.status, rec.Code)
		if tc.status == http.StatusOK {
			require.Contains(t, rec.Body.String(), `"items":[]`)
		}
	}
	settings.values[service.SettingKeyChannelMonitorMode] = "v1"
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodGet, "/", nil)
	require.True(t, h.featureEnabled(c), "standalone V1 keeps its user-visible API")
}
