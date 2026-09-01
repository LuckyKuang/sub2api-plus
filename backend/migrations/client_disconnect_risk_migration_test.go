package migrations

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestClientDisconnectRiskMigrationDefinesDefaultsAndOrderedState(t *testing.T) {
	content, err := FS.ReadFile("238_client_disconnect_risk.sql")
	require.NoError(t, err)

	sql := strings.Join(strings.Fields(string(content)), " ")
	require.Contains(t, sql, "'client_disconnect_consecutive_ban_enabled', 'true'")
	require.Contains(t, sql, "'client_disconnect_consecutive_ban_threshold', '10'")
	require.Contains(t, sql, "UNIQUE (user_id, generation, request_id)")
	require.Contains(t, sql, "PRIMARY KEY (user_id, generation, sequence)")
	require.Contains(t, sql, "CHECK (threshold IS NULL OR threshold BETWEEN 1 AND 1000)")
	require.Contains(t, sql, "OLD.status IS DISTINCT FROM 'active' AND NEW.status = 'active'")
	require.Contains(t, sql, "OLD.role IS DISTINCT FROM 'admin' AND NEW.role = 'admin'")
}
