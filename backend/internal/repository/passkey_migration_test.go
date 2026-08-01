package repository

import (
	"testing"

	"github.com/Wei-Shaw/sub2api/migrations"
	"github.com/stretchr/testify/require"
)

func TestPasskeyMigrationUsesNewPlusIdentityAndIdempotentDDL(t *testing.T) {
	legacy191, err := migrations.FS.ReadFile("191_add_group_allow_live.sql")
	require.NoError(t, err)
	require.Contains(t, string(legacy191), "allow_live")

	passkeyDDL, err := migrations.FS.ReadFile("196_passkey_credentials.sql")
	require.NoError(t, err)
	ddl := string(passkeyDDL)
	for _, fragment := range []string{
		"CREATE TABLE IF NOT EXISTS passkey_user_handles",
		"CREATE TABLE IF NOT EXISTS passkey_credentials",
		"CREATE INDEX IF NOT EXISTS passkey_credentials_user_id_idx",
		"CREATE INDEX IF NOT EXISTS passkey_credentials_last_used_at_idx",
	} {
		require.Contains(t, ddl, fragment)
	}
}
