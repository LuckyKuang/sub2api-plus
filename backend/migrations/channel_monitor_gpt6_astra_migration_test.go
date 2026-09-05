package migrations

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestChannelMonitorGPT6AstraMigration(t *testing.T) {
	content, err := FS.ReadFile("251_channel_monitor_gpt6_astra.sql")
	require.NoError(t, err)

	sql := strings.Join(strings.Fields(string(content)), " ")
	require.Contains(t, sql, "jsonb_build_array('gpt-6-astra') || (platform->'models')")
	require.Contains(t, sql, "updated_by IS NULL")
	require.Contains(t, sql, "platform->'models' @> '[\"gpt-5.6-sol\"]'::jsonb")
	require.Contains(t, sql, "NOT (platform->'models' @> '[\"gpt-6-astra\"]'::jsonb)")
	require.Contains(t, sql, "version = version + 1")
}
