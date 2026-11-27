//go:build unit

package admin

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/LuckyKuang/sub2api-plus/internal/handler/dto"
	"github.com/stretchr/testify/require"
)

func TestUpdateSettingsChannelMonitorProbeTimeouts(t *testing.T) {
	const headerKey = "channel_monitor_probe_response_header_timeout_seconds"
	const totalKey = "channel_monitor_probe_total_timeout_seconds"
	h, repo := newStepUpSwitchTestHandler(t, map[string]string{})

	for _, step := range []struct {
		fields map[string]any
		header int
		total  int
	}{
		{map[string]any{"site_name": "Gateway"}, 30, 45},
		{map[string]any{headerKey: 60, totalKey: 90}, 60, 90},
		{map[string]any{totalKey: 120}, 60, 120},
		{map[string]any{headerKey: nil}, 60, 120},
	} {
		rec := doUpdateSettings(t, h, step.fields, nil)
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		var body struct {
			Data dto.SystemSettings `json:"data"`
		}
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
		require.Equal(t, step.header, body.Data.ChannelMonitorProbeResponseHeaderTimeoutSeconds)
		require.Equal(t, step.total, body.Data.ChannelMonitorProbeTotalTimeoutSeconds)
	}
	require.Equal(t, "60", repo.values[headerKey])
	require.Equal(t, "120", repo.values[totalKey])
	require.NotContains(t, repo.lastUpdates, headerKey)
	require.NotContains(t, repo.lastUpdates, totalKey)
}

func TestUpdateSettingsChannelMonitorProbeTimeoutsRejectsInvalidPair(t *testing.T) {
	const headerKey = "channel_monitor_probe_response_header_timeout_seconds"
	const totalKey = "channel_monitor_probe_total_timeout_seconds"
	for _, fields := range []map[string]any{
		{headerKey: 0, totalKey: 0},
		{headerKey: -1},
		{headerKey: 121, totalKey: 121},
		{headerKey: 31.5},
		{totalKey: 29},
	} {
		h, repo := newStepUpSwitchTestHandler(t, map[string]string{headerKey: "30", totalKey: "45"})
		rec := doUpdateSettings(t, h, fields, nil)
		require.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
		require.Empty(t, repo.lastUpdates)
	}
}
