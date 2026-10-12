//go:build unit || !integration

package migrations

import (
	"github.com/stretchr/testify/require"
	"strings"
	"testing"
)

// Existing billable service_tier cannot reconstruct historical diagnostics.
func TestUsageServiceTierSnapshotsPreserveUnknownHistory(t *testing.T) {
	content, err := FS.ReadFile("284_usage_service_tier_snapshots.sql")
	require.NoError(t, err)
	sql := strings.ToUpper(string(content))
	require.Contains(t, sql, "ADD COLUMN IF NOT EXISTS REQUESTED_SERVICE_TIER VARCHAR(64)")
	require.Contains(t, sql, "ADD COLUMN IF NOT EXISTS RESPONSE_SERVICE_TIER VARCHAR(64)")
	require.NotContains(t, sql, "NOT NULL")
	require.NotContains(t, sql, " DEFAULT ")
	require.NotContains(t, sql, "UPDATE USAGE_LOGS")
}
