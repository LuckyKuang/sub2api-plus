package migrations

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMigration197AddsUsageLogTPSMetadata(t *testing.T) {
	content, err := FS.ReadFile("197_add_usage_log_tps_metadata.sql")
	require.NoError(t, err)

	sql := strings.Join(strings.Fields(string(content)), " ")
	require.Contains(t, sql, "ADD COLUMN IF NOT EXISTS audio_output_tokens INTEGER NOT NULL DEFAULT 0")
	require.Contains(t, sql, "ADD COLUMN IF NOT EXISTS is_complete BOOLEAN")
	require.Contains(t, sql, "COMMENT ON COLUMN usage_logs.is_complete")

	// Historical rows intentionally remain unknown so the UI never reports
	// a misleading TPS value for records created before completion tracking.
	require.NotContains(t, strings.ToUpper(sql), "UPDATE USAGE_LOGS")
}
