//go:build unit || !integration

package migrations

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestFirstByteMigrationDoesNotInventHistoricalMeasurements(t *testing.T) {
	raw, err := FS.ReadFile("285_first_byte_timing.sql")
	require.NoError(t, err)
	sql := string(raw)
	require.Equal(t, 2, strings.Count(sql, "first_byte_ms INTEGER"))
	require.Contains(t, sql, "CHECK (first_byte_ms >= 0)")
	require.NotContains(t, strings.ToUpper(sql), "UPDATE USAGE_LOGS")
	require.NotContains(t, strings.ToUpper(sql), "UPDATE CHANNEL_MONITOR_HISTORIES")
	require.NotContains(t, sql, "first_byte_ms INTEGER NOT NULL")
	require.Contains(t, sql, "count_first_byte INTEGER NOT NULL DEFAULT 0")
}
