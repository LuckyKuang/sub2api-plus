//go:build integration

package repository

import (
	"context"
	"testing"

	dbmigrations "github.com/LuckyKuang/sub2api-plus/migrations"
	"github.com/stretchr/testify/require"
)

func TestMigration224EnforcesExplicitCodexFingerprintMode(t *testing.T) {
	tx := testTx(t)
	ctx := context.Background()
	migrationSQL, err := dbmigrations.FS.ReadFile("224_backfill_codex_fingerprint_mode.sql")
	require.NoError(t, err)

	_, err = tx.ExecContext(ctx, `
DROP TRIGGER IF EXISTS accounts_enforce_codex_fingerprint_mode_extra ON accounts;
`)
	require.NoError(t, err)

	var missingID int64
	require.NoError(t, tx.QueryRowContext(ctx, `
INSERT INTO accounts (name, platform, type, extra)
VALUES ('migration-224-missing', 'openai', 'oauth', '{}'::jsonb)
RETURNING id
`).Scan(&missingID))

	var explicitSessionID int64
	require.NoError(t, tx.QueryRowContext(ctx, `
INSERT INTO accounts (name, platform, type, extra)
VALUES ('migration-224-session', 'openai', 'oauth', '{"codex_fingerprint_mode":"session"}'::jsonb)
RETURNING id
`).Scan(&explicitSessionID))

	var malformedID int64
	require.NoError(t, tx.QueryRowContext(ctx, `
INSERT INTO accounts (name, platform, type, extra)
VALUES ('migration-224-malformed', 'openai', 'oauth', '{"codex_fingerprint_mode":false}'::jsonb)
RETURNING id
`).Scan(&malformedID))

	var shadowID int64
	require.NoError(t, tx.QueryRowContext(ctx, `
INSERT INTO accounts (name, platform, type, extra, parent_account_id, quota_dimension)
VALUES ('migration-224-shadow', 'openai', 'oauth', '{"codex_fingerprint_mode":"session"}'::jsonb, $1, 'spark')
RETURNING id
`, explicitSessionID).Scan(&shadowID))

	var apiKeyID int64
	require.NoError(t, tx.QueryRowContext(ctx, `
INSERT INTO accounts (name, platform, type, extra)
VALUES ('migration-224-apikey', 'openai', 'apikey', '{"codex_fingerprint_mode":"session"}'::jsonb)
RETURNING id
`).Scan(&apiKeyID))

	_, err = tx.ExecContext(ctx, string(migrationSQL))
	require.NoError(t, err)
	_, err = tx.ExecContext(ctx, string(migrationSQL))
	require.NoError(t, err)

	assertMode := func(id int64, expected string) {
		t.Helper()
		var mode string
		require.NoError(t, tx.QueryRowContext(ctx, `
SELECT extra->>'codex_fingerprint_mode' FROM accounts WHERE id = $1
`, id).Scan(&mode))
		require.Equal(t, expected, mode)
	}
	assertMode(missingID, "device")
	assertMode(explicitSessionID, "session")
	assertMode(malformedID, "device")

	var shadowHasMode bool
	require.NoError(t, tx.QueryRowContext(ctx, `
SELECT extra ? 'codex_fingerprint_mode' FROM accounts WHERE id = $1
`, shadowID).Scan(&shadowHasMode))
	require.False(t, shadowHasMode)
	var apiKeyHasMode bool
	require.NoError(t, tx.QueryRowContext(ctx, `
SELECT extra ? 'codex_fingerprint_mode' FROM accounts WHERE id = $1
`, apiKeyID).Scan(&apiKeyHasMode))
	require.False(t, apiKeyHasMode)

	_, err = tx.ExecContext(ctx, `
UPDATE accounts SET extra = extra - 'codex_fingerprint_mode' WHERE id = $1
`, explicitSessionID)
	require.NoError(t, err)
	assertMode(explicitSessionID, "session")
	_, err = tx.ExecContext(ctx, `
UPDATE accounts
SET extra = jsonb_set(extra, '{codex_fingerprint_mode}', 'null'::jsonb, true)
WHERE id = $1
`, explicitSessionID)
	require.NoError(t, err)
	assertMode(explicitSessionID, "device")

	var insertedMode string
	require.NoError(t, tx.QueryRowContext(ctx, `
INSERT INTO accounts (name, platform, type, extra)
VALUES ('migration-224-new-default', 'openai', 'oauth', '{}'::jsonb)
RETURNING extra->>'codex_fingerprint_mode'
`).Scan(&insertedMode))
	require.Equal(t, "device", insertedMode)

	var apiKeyRetainedMode bool
	require.NoError(t, tx.QueryRowContext(ctx, `
INSERT INTO accounts (name, platform, type, extra)
VALUES ('migration-224-new-apikey', 'openai', 'apikey', '{"codex_fingerprint_mode":"device"}'::jsonb)
RETURNING extra ? 'codex_fingerprint_mode'
`).Scan(&apiKeyRetainedMode))
	require.False(t, apiKeyRetainedMode)

	_, err = tx.ExecContext(ctx, `
INSERT INTO accounts (name, platform, type, extra)
VALUES ('migration-224-invalid', 'openai', 'oauth', '{"codex_fingerprint_mode":"invalid"}'::jsonb)
`)
	require.ErrorContains(t, err, "codex_fingerprint_mode must be one of")
}
