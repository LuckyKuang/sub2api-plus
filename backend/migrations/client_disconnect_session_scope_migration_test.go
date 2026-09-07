package migrations

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestClientDisconnectSessionScopeMigration(t *testing.T) {
	content, err := FS.ReadFile("256_client_disconnect_session_scope.sql")
	require.NoError(t, err)
	sql := strings.ToLower(string(content))

	require.Contains(t, sql, "client_disconnect_consecutive_ban_enabled")
	require.Contains(t, sql, "set value = 'false'")
	require.Contains(t, sql, "client_disconnect_consecutive_ban_generation")
	require.Contains(t, sql, "add column if not exists session_id varchar(255)")
	require.Contains(t, sql, "add column if not exists session_scope varchar(80)")
	require.Contains(t, sql, "add column if not exists user_email varchar(255)")
	require.Contains(t, sql, "add column if not exists api_key_name varchar(255)")
	require.Contains(t, sql, "primary key (user_id, session_scope, generation, sequence)")
	require.Contains(t, sql, "primary key (user_id, session_scope)")
	require.Contains(t, sql, "drop table if exists client_disconnect_risk_states")
	require.Contains(t, sql, "when btrim(value) ~ '^[0-9]+$'")
	require.Contains(t, sql, "btrim(value)::numeric between 1 and 9223372036854775806")
	require.Contains(t, sql, "idx_client_disconnect_risk_events_request_id")
	require.Contains(t, sql, "idx_client_disconnect_risk_events_session_id_accepted")
	require.Contains(t, sql, "idx_client_disconnect_risk_events_accepted_order")
}
