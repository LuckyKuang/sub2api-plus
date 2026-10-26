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

func TestChannelMonitorV2ModelVisibilityMigrationPreservesPoliciesAndHistory(t *testing.T) {
	dsn := os.Getenv("SUB2API_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("requires isolated PostgreSQL via SUB2API_TEST_POSTGRES_DSN")
	}
	db, err := sql.Open("postgres", dsn)
	require.NoError(t, err)
	defer db.Close()
	tx, err := db.BeginTx(context.Background(), nil)
	require.NoError(t, err)
	defer tx.Rollback()
	_, err = tx.Exec(`CREATE TEMP TABLE channel_monitor_v2_config (
		id int, version int DEFAULT 1, platforms jsonb DEFAULT '[]', updated_by bigint, updated_at timestamptz DEFAULT NOW()
	) ON COMMIT DROP`)
	require.NoError(t, err)
	before := `[
		{"platform":"openai","enabled":true,"models":["Named"],"custom":"keep"},
		{"platform":"custom","enabled":false,"models":[],"excluded_models":null},
		{"platform":"grok","enabled":true,"models":[],"model_display_mode":"selected","excluded_models":["Hidden"]}
	]`
	_, err = tx.Exec(`INSERT INTO channel_monitor_v2_config(id,version,platforms,updated_by) VALUES(1,7,$1,99)`, before)
	require.NoError(t, err)
	migration, err := FS.ReadFile("295_channel_monitor_v2_model_visibility.sql")
	require.NoError(t, err)
	for range 2 {
		_, err = tx.Exec(string(migration))
		require.NoError(t, err)
	}
	var platforms string
	var version int
	var updatedBy int64
	require.NoError(t, tx.QueryRow(`SELECT platforms::text,version,updated_by FROM channel_monitor_v2_config WHERE id=1`).Scan(&platforms, &version, &updatedBy))
	require.JSONEq(t, `[
		{"platform":"openai","enabled":true,"models":["Named"],"custom":"keep","model_display_mode":"selected","excluded_models":[]},
		{"platform":"custom","enabled":false,"models":[],"model_display_mode":"all","excluded_models":[]},
		{"platform":"grok","enabled":true,"models":[],"model_display_mode":"selected","excluded_models":["Hidden"]}
	]`, platforms)
	require.Equal(t, 8, version, "idempotent replay must not advance the optimistic lock twice")
	require.Equal(t, int64(99), updatedBy)
	_, err = tx.Exec(`INSERT INTO channel_monitor_v2_config(id) VALUES(2)`)
	require.NoError(t, err)
	var missingDefaults int
	require.NoError(t, tx.QueryRow(`SELECT COUNT(*) FROM channel_monitor_v2_config c, jsonb_array_elements(c.platforms) p WHERE c.id=2 AND (p->'excluded_models' <> '[]'::jsonb OR p->>'model_display_mode' <> 'all' OR NOT p ? 'excluded_models')`).Scan(&missingDefaults))
	require.Zero(t, missingDefaults)
}
