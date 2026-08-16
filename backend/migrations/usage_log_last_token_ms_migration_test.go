package migrations

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMigration221AddsUsageLogLastTokenMs(t *testing.T) {
	content, err := FS.ReadFile("221_add_usage_log_last_token_ms.sql")
	require.NoError(t, err)

	sql := strings.Join(strings.Fields(string(content)), " ")
	require.Contains(t, sql, "ADD COLUMN IF NOT EXISTS last_token_ms INTEGER")
	require.Contains(t, sql, "COMMENT ON COLUMN usage_logs.last_token_ms")

	// Historical rows stay NULL so the UI never invents a last-token sample.
	require.NotContains(t, strings.ToUpper(sql), "UPDATE USAGE_LOGS")
}
