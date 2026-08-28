package migrations

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDropUpstreamBillingProbeMigrationLeavesRateMultiplierUntouched(t *testing.T) {
	content, err := FS.ReadFile("234_drop_upstream_billing_probe.sql")
	require.NoError(t, err)

	sql := strings.Join(strings.Fields(string(content)), " ")
	require.Contains(t, sql, "upstream_billing_probe")
	require.Contains(t, sql, "upstream_billing_probe_enabled")
	require.Contains(t, sql, "upstream_billing_rate_sync_enabled")
	require.Contains(t, sql, "DELETE FROM settings WHERE key = 'upstream_billing_probe_settings'")
	require.Contains(t, sql, "INSERT INTO scheduler_outbox")
	executable := strings.ToUpper(sql)
	if idx := strings.Index(executable, "WITH UPDATED_ACCOUNTS"); idx >= 0 {
		executable = executable[idx:]
	}
	require.NotContains(t, executable, "RATE_MULTIPLIER")
}
