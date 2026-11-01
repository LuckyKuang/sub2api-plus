//go:build unit || !integration

package migrations

import (
	"github.com/stretchr/testify/require"
	"strings"
	"testing"
)

func TestUsageOutboundMetadataPreservesUnknownHistory(t *testing.T) {
	content, err := FS.ReadFile("304_usage_outbound_metadata.sql")
	require.NoError(t, err)
	sql := strings.ToUpper(string(content))
	require.Contains(t, sql, "ADD COLUMN IF NOT EXISTS OUTBOUND_METADATA JSONB")
	require.NotContains(t, sql, "UPDATE USAGE_LOGS")
	require.NotContains(t, sql, " DEFAULT ")
	require.Contains(t, sql, "'GATEWAY_SEND'")
	require.Contains(t, sql, "'CLI_PROCESS'")
}
