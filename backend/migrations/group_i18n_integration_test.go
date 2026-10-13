//go:build integration

package migrations

import (
	"context"
	"database/sql"
	"os"
	"testing"

	_ "github.com/lib/pq"
	"github.com/stretchr/testify/require"
)

func TestGroupI18nUpgradePreservesPlusGroupData(t *testing.T) {
	dsn := os.Getenv("SUB2API_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("requires isolated PostgreSQL via SUB2API_TEST_POSTGRES_DSN")
	}
	db, err := sql.Open("postgres", dsn)
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	ctx := context.Background()
	tx, err := db.BeginTx(ctx, nil)
	require.NoError(t, err)
	defer func() { _ = tx.Rollback() }()
	_, err = tx.ExecContext(ctx, `CREATE TEMP TABLE groups (
		id bigint PRIMARY KEY, name text, description text, five_hour_limit_usd numeric,
		quota_reset_source_account_id bigint, model_pricing jsonb) ON COMMIT DROP;
		INSERT INTO groups VALUES (1,'原名称','原描述',12.50,42,'[{"model":"custom","input_price":0}]');`)
	require.NoError(t, err)
	migration, err := FS.ReadFile("286_group_i18n.sql")
	require.NoError(t, err)
	_, err = tx.ExecContext(ctx, string(migration))
	require.NoError(t, err)
	var name, description, pricing, translations string
	var limit float64
	var source int64
	require.NoError(t, tx.QueryRowContext(ctx, `SELECT name,description,five_hour_limit_usd,
		quota_reset_source_account_id,model_pricing::text,i18n::text FROM groups WHERE id=1`).
		Scan(&name, &description, &limit, &source, &pricing, &translations))
	require.Equal(t, "原名称", name)
	require.Equal(t, "原描述", description)
	require.Equal(t, 12.50, limit)
	require.Equal(t, int64(42), source)
	require.JSONEq(t, `[{"model":"custom","input_price":0}]`, pricing)
	require.JSONEq(t, `{}`, translations)
	_, err = tx.ExecContext(ctx, `UPDATE groups SET i18n='{"en":{"name":"Translated"}}' WHERE id=1`)
	require.NoError(t, err)
	// A repeated forward migration must keep translations already saved.
	_, err = tx.ExecContext(ctx, string(migration))
	require.NoError(t, err)
	require.NoError(t, tx.QueryRowContext(ctx, `SELECT i18n::text FROM groups WHERE id=1`).Scan(&translations))
	require.JSONEq(t, `{"en":{"name":"Translated"}}`, translations)
	_, err = tx.ExecContext(ctx, `INSERT INTO groups (id,name) VALUES (2,'New')`)
	require.NoError(t, err)
	require.NoError(t, tx.QueryRowContext(ctx, `SELECT i18n::text FROM groups WHERE id=2`).Scan(&translations))
	require.JSONEq(t, `{}`, translations)
}
