//go:build integration

package repository

import (
	"context"
	"testing"

	dbmigrations "github.com/LuckyKuang/sub2api-plus/migrations"
	"github.com/stretchr/testify/require"
)

func TestCompositeConversionMigrationRequiresExplicitConsent(t *testing.T) {
	ctx := context.Background()
	tx := testTx(t)
	_, err := tx.ExecContext(ctx, `CREATE TEMP TABLE api_keys (id BIGINT PRIMARY KEY, default_client VARCHAR(32) NOT NULL DEFAULT '', default_endpoint VARCHAR(64) NOT NULL DEFAULT '') ON COMMIT DROP;
		INSERT INTO api_keys (id, default_client, default_endpoint) VALUES (1, 'codex', '/v1/responses')`)
	require.NoError(t, err)
	migration, err := dbmigrations.FS.ReadFile("306_api_key_composite_conversion.sql")
	require.NoError(t, err)
	_, err = tx.ExecContext(ctx, string(migration))
	require.NoError(t, err)
	var enabled bool
	var endpoint string
	require.NoError(t, tx.QueryRowContext(ctx, "SELECT conversion_enabled, default_endpoint FROM api_keys WHERE id=1").Scan(&enabled, &endpoint))
	require.False(t, enabled, "legacy endpoint preferences cannot authorize conversion")
	require.Equal(t, "/v1/responses", endpoint)
	_, err = tx.ExecContext(ctx, "INSERT INTO api_keys (id) VALUES (2)")
	require.NoError(t, err)
	require.NoError(t, tx.QueryRowContext(ctx, "SELECT conversion_enabled FROM api_keys WHERE id=2").Scan(&enabled))
	require.False(t, enabled, "new keys also default to native access")
	_, err = tx.ExecContext(ctx, "UPDATE api_keys SET conversion_enabled=true WHERE id=1")
	require.NoError(t, err)
	_, err = tx.ExecContext(ctx, string(migration))
	require.NoError(t, err)
	require.NoError(t, tx.QueryRowContext(ctx, "SELECT conversion_enabled FROM api_keys WHERE id=1").Scan(&enabled))
	require.True(t, enabled, "idempotent migration preserves explicit consent")
}
