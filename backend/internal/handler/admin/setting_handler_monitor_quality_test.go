//go:build unit

package admin

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/LuckyKuang/sub2api-plus/internal/handler/dto"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// Both merged feature contracts must survive one settings save, subsequent
// unrelated partial updates, and the independent GET response.
func TestUpdateSettingsChannelMonitorAndQualityInspectionCoexist(t *testing.T) {
	h, repo := newStepUpSwitchTestHandler(t, map[string]string{})
	rec := doUpdateSettings(t, h, map[string]any{
		"channel_monitor_probe_response_header_timeout_seconds": 60,
		"channel_monitor_probe_total_timeout_seconds":           90,
		"model_quality_inspection_enabled":                      true,
		"quality_inspection_max_requests_per_round":             40,
	}, nil)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	check := func(raw []byte) {
		var body struct {
			Data dto.SystemSettings `json:"data"`
		}
		require.NoError(t, json.Unmarshal(raw, &body))
		require.True(t, body.Data.ModelQualityInspectionEnabled)
		require.Equal(t, 40, body.Data.QualityInspectionMaxRequestsPerRound)
		require.Equal(t, 60, body.Data.ChannelMonitorProbeResponseHeaderTimeoutSeconds)
		require.Equal(t, 90, body.Data.ChannelMonitorProbeTotalTimeoutSeconds)
	}
	check(rec.Body.Bytes())
	rec = doUpdateSettings(t, h, map[string]any{"site_name": "Review gateway"}, nil)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	check(rec.Body.Bytes())
	require.Equal(t, "60", repo.values["channel_monitor_probe_response_header_timeout_seconds"])
	require.Equal(t, "true", repo.values["model_quality_inspection_enabled"])
	get := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(get)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/admin/settings", nil)
	h.GetSettings(c)
	require.Equal(t, http.StatusOK, get.Code, get.Body.String())
	check(get.Body.Bytes())
}
