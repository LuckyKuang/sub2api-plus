package migrations

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMigration219AddsRoutingDiagnosticsWithoutChangingBusinessLimited(t *testing.T) {
	content, err := FS.ReadFile("219_ops_error_routing_diagnostics.sql")
	require.NoError(t, err)

	sql := strings.Join(strings.Fields(string(content)), " ")
	require.Contains(t, sql, "ADD COLUMN IF NOT EXISTS is_routing_capacity_limited BOOLEAN NOT NULL DEFAULT FALSE")
	require.Contains(t, sql, "ADD COLUMN IF NOT EXISTS routing_diagnostics JSONB")
	require.Contains(t, sql, "UPDATE ops_error_logs SET is_routing_capacity_limited = TRUE WHERE COALESCE(error_phase, '') = 'routing'")
	require.NotContains(t, sql, "UPDATE ops_error_logs SET is_business_limited")
}
