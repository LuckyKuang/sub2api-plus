package migrations

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestClientDisconnectLifecycleObservabilityMigration(t *testing.T) {
	content, err := FS.ReadFile("246_client_disconnect_lifecycle_observability.sql")
	require.NoError(t, err)
	sql := strings.Join(strings.Fields(string(content)), " ")
	require.Contains(t, sql, "completion_status VARCHAR(32)")
	require.Contains(t, sql, "usage_source VARCHAR(32)")
	require.Contains(t, sql, "usage_missing BOOLEAN NOT NULL DEFAULT TRUE")
	require.Contains(t, sql, "WHERE usage_missing = TRUE")
}
