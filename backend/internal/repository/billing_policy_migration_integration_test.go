//go:build integration

package repository

import (
	"context"
	"testing"

	dbmigrations "github.com/LuckyKuang/sub2api-plus/migrations"
	"github.com/stretchr/testify/require"
)

func TestBillingPolicyMigrationPreservesHistoricalEvidence(t *testing.T) {
	ctx := context.Background()
	tx, err := integrationDB.BeginTx(ctx, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = tx.Rollback() })
	// Temporary tables model pre-upgrade rows without changing the real schema.
	for _, table := range []string{"groups", "usage_logs", "payment_orders", "batch_image_jobs"} {
		_, err = tx.ExecContext(ctx, "CREATE TEMP TABLE "+table+" (id BIGINT PRIMARY KEY) ON COMMIT DROP")
		require.NoError(t, err)
		_, err = tx.ExecContext(ctx, "INSERT INTO "+table+" (id) VALUES (1)")
		require.NoError(t, err)
	}
	migration, err := dbmigrations.FS.ReadFile("299_composite_billing_rate_policy.sql")
	require.NoError(t, err)
	_, err = tx.ExecContext(ctx, string(migration))
	require.NoError(t, err)
	var policy string
	var version int64
	require.NoError(t, tx.QueryRowContext(ctx, "SELECT billing_rate_policy::text, billing_policy_version FROM groups WHERE id=1").Scan(&policy, &version))
	require.JSONEq(t, "{}", policy)
	require.EqualValues(t, 1, version)
	var absent bool
	require.NoError(t, tx.QueryRowContext(ctx, "SELECT billing_policy_version, billing_rate_snapshot IS NULL FROM usage_logs WHERE id=1").Scan(&version, &absent))
	require.Zero(t, version)
	require.True(t, absent)
	require.NoError(t, tx.QueryRowContext(ctx, "SELECT billing_rate_snapshot::text FROM batch_image_jobs WHERE id=1").Scan(&policy))
	require.JSONEq(t, "{}", policy)
	require.NoError(t, tx.QueryRowContext(ctx, "SELECT pricing_policy_version, pricing_summary IS NULL FROM payment_orders WHERE id=1").Scan(&version, &absent))
	require.Zero(t, version)
	require.True(t, absent)
	_, err = tx.ExecContext(ctx, `UPDATE groups SET billing_rate_policy='{"platform_multipliers":{"openai":0}}', billing_policy_version=7 WHERE id=1`)
	require.NoError(t, err)
	_, err = tx.ExecContext(ctx, string(migration))
	require.NoError(t, err)
	require.NoError(t, tx.QueryRowContext(ctx, "SELECT billing_rate_policy::text, billing_policy_version FROM groups WHERE id=1").Scan(&policy, &version))
	require.JSONEq(t, `{"platform_multipliers":{"openai":0}}`, policy)
	require.EqualValues(t, 7, version)
}
