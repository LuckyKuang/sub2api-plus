//go:build integration

package repository

import (
	"context"
	"testing"

	dbmigrations "github.com/LuckyKuang/sub2api-plus/migrations"
	"github.com/stretchr/testify/require"
)

func TestMigration234DropsProbeMetadataWithoutChangingManualRate(t *testing.T) {
	tx := testTx(t)
	ctx := context.Background()
	migrationSQL, err := dbmigrations.FS.ReadFile("234_drop_upstream_billing_probe.sql")
	require.NoError(t, err)

	var accountID int64
	require.NoError(t, tx.QueryRowContext(ctx, `
INSERT INTO accounts (name, platform, type, rate_multiplier, extra)
VALUES (
    'migration-234-account',
    'openai',
    'apikey',
    0.4,
    '{
      "upstream_billing_probe":{"status":"ok"},
      "upstream_billing_probe_enabled":true,
      "upstream_billing_rate_sync_enabled":true,
      "retained":"value"
    }'::jsonb
)
RETURNING id
`).Scan(&accountID))

	_, err = tx.ExecContext(ctx, `
INSERT INTO settings (key, value)
VALUES ('upstream_billing_probe_settings', '{"enabled":true}')
ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value
`)
	require.NoError(t, err)
	_, err = tx.ExecContext(ctx, "DELETE FROM scheduler_outbox WHERE account_id = $1", accountID)
	require.NoError(t, err)

	_, err = tx.ExecContext(ctx, string(migrationSQL))
	require.NoError(t, err)

	var (
		rate  float64
		extra string
	)
	require.NoError(t, tx.QueryRowContext(ctx, `
SELECT rate_multiplier, extra::text
FROM accounts
WHERE id = $1
`, accountID).Scan(&rate, &extra))
	require.InDelta(t, 0.4, rate, 0.00001)
	require.JSONEq(t, `{"retained":"value"}`, extra)

	var settingCount int
	require.NoError(t, tx.QueryRowContext(ctx, `
SELECT COUNT(*) FROM settings WHERE key = 'upstream_billing_probe_settings'
`).Scan(&settingCount))
	require.Zero(t, settingCount)

	var outboxCount int
	require.NoError(t, tx.QueryRowContext(ctx, `
SELECT COUNT(*)
FROM scheduler_outbox
WHERE event_type = 'account_changed' AND account_id = $1
`, accountID).Scan(&outboxCount))
	require.Equal(t, 1, outboxCount)
}
