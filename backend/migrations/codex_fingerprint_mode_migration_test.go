package migrations

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMigration224BackfillsAndEnforcesExplicitCodexFingerprintMode(t *testing.T) {
	content, err := FS.ReadFile("224_backfill_codex_fingerprint_mode.sql")
	require.NoError(t, err)

	sql := string(content)
	require.Contains(t, sql, "parent_account_id IS NULL")
	require.Contains(t, sql, "type = 'oauth'")
	require.Contains(t, sql, "ELSE 'device'")
	require.Contains(t, sql, "codex_fingerprint_mode")
	require.Contains(t, sql, "BEFORE INSERT OR UPDATE")
	require.Contains(t, sql, "CREATE TRIGGER")
	require.Contains(t, sql, "must be one of off, device, session, full")
}
