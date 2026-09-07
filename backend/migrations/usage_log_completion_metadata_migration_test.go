package migrations

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestUsageLogCompletionMetadataMigration(t *testing.T) {
	content, err := FS.ReadFile("247_usage_log_completion_metadata.sql")
	require.NoError(t, err)
	sql := strings.Join(strings.Fields(string(content)), " ")
	require.Contains(t, sql, "completion_status VARCHAR(32) NOT NULL DEFAULT 'unknown'")
	require.Contains(t, sql, "usage_source VARCHAR(32) NOT NULL DEFAULT 'unknown'")
	require.Contains(t, sql, "'client_disconnected'")
	require.Contains(t, sql, "'reconciled'")
}
