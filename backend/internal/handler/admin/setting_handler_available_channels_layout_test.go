//go:build unit

package admin

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/LuckyKuang/sub2api-plus/internal/service"
	"github.com/stretchr/testify/require"
)

// docs/AVAILABLE_CHANNELS.md: the administrator selects the site-wide layout;
// older clients omitting this field must preserve it. Bad inputs write nothing.
func TestUpdateSettingsAvailableChannelsLayout(t *testing.T) {
	h, repo := newStepUpSwitchTestHandler(t, map[string]string{})
	for _, layout := range []string{"list", "card", "list"} {
		rec := doUpdateSettings(t, h, map[string]any{"available_channels_layout": layout}, nil)
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		require.Equal(t, layout, repo.values[service.SettingKeyAvailableChannelsLayout])
		var body struct {
			Data struct {
				Layout string `json:"available_channels_layout"`
			} `json:"data"`
		}
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
		require.Equal(t, layout, body.Data.Layout)
	}
	rec := doUpdateSettings(t, h, map[string]any{"site_name": "Layout preserved"}, nil)
	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, "list", repo.values[service.SettingKeyAvailableChannelsLayout])
	rec = doUpdateSettings(t, h, map[string]any{"available_channels_layout": nil}, nil)
	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, "list", repo.values[service.SettingKeyAvailableChannelsLayout])
	for _, invalid := range []any{"", "LIST", "table", 42} {
		before := make(map[string]string, len(repo.values))
		for key, value := range repo.values {
			before[key] = value
		}
		rec := doUpdateSettings(t, h, map[string]any{"available_channels_layout": invalid, "site_name": "Must not save"}, nil)
		require.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
		require.Equal(t, before, repo.values)
	}
}

// The operation log must identify layout changes without recording payloads.
func TestAvailableChannelsLayoutSettingsAudit(t *testing.T) {
	before := &service.SystemSettings{AvailableChannelsLayout: "card"}
	after := &service.SystemSettings{AvailableChannelsLayout: "list"}
	require.Contains(t, diffSettings(before, after, nil, nil, UpdateSettingsRequest{}), "available_channels_layout")
	require.NotContains(t, diffSettings(after, after, nil, nil, UpdateSettingsRequest{}), "available_channels_layout")
}

func TestUpdateSettingsOmittedLayoutDoesNotRewriteStorage(t *testing.T) {
	// Unknown stored values are read as cards, but an older client saving a
	// different field must not persist its normalized snapshot over storage.
	h, repo := newStepUpSwitchTestHandler(t, map[string]string{
		service.SettingKeyAvailableChannelsLayout: "unknown",
	})
	rec := doUpdateSettings(t, h, map[string]any{"site_name": "Unrelated save"}, nil)
	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, "unknown", repo.values[service.SettingKeyAvailableChannelsLayout])
}
