package migrations

import (
	"github.com/stretchr/testify/require"
	"strings"
	"testing"
)

func TestUsageTimingVersionMigrationPreservesHistoricalSamples(t *testing.T) {
	content, err := FS.ReadFile("257_usage_timing_version.sql")
	require.NoError(t, err)
	sql := string(content)
	require.Contains(t, sql, "timing_version INTEGER NOT NULL DEFAULT 0")
	require.NotContains(t, strings.ToUpper(sql), "UPDATE USAGE_LOGS")
	require.Contains(t, sql, "'compaction'")
	require.Contains(t, sql, "DELETE FROM settings WHERE key = 'openai_ttft_mode'")
}
