//go:build unit

package admin

import (
	"net/http"
	"testing"

	"github.com/LuckyKuang/sub2api-plus/internal/service"
	"github.com/stretchr/testify/require"
)

func TestUpdateSettings_VersionBoundsValidateEffectivePartialState(t *testing.T) {
	t.Run("Codex min cannot exceed retained max", func(t *testing.T) {
		h, repo := newStepUpSwitchTestHandler(t, map[string]string{
			service.SettingKeyMaxCodexVersion: "0.150.0",
		})

		rec := doUpdateSettings(t, h, map[string]any{"min_codex_version": "0.200.0"}, nil)
		require.Equal(t, http.StatusBadRequest, rec.Code)
		require.Equal(t, "0.150.0", repo.values[service.SettingKeyMaxCodexVersion])
	})

	t.Run("Codex max cannot precede retained min", func(t *testing.T) {
		h, repo := newStepUpSwitchTestHandler(t, map[string]string{
			service.SettingKeyMinCodexVersion: "0.200.0",
		})

		rec := doUpdateSettings(t, h, map[string]any{"max_codex_version": "0.150.0"}, nil)
		require.Equal(t, http.StatusBadRequest, rec.Code)
		require.Equal(t, "0.200.0", repo.values[service.SettingKeyMinCodexVersion])
	})

	t.Run("Claude Code bounds use SemVer prerelease precedence", func(t *testing.T) {
		h, _ := newStepUpSwitchTestHandler(t, map[string]string{})

		rec := doUpdateSettings(t, h, map[string]any{
			"min_claude_code_version": "0.147.0",
			"max_claude_code_version": "0.147.0-alpha.4",
		}, nil)
		require.Equal(t, http.StatusBadRequest, rec.Code)
	})

	t.Run("strict SemVer rejects malformed Codex profile bound", func(t *testing.T) {
		h, _ := newStepUpSwitchTestHandler(t, map[string]string{})

		rec := doUpdateSettings(t, h, map[string]any{"min_codex_version": "01.2.3"}, nil)
		require.Equal(t, http.StatusBadRequest, rec.Code)
	})
}
