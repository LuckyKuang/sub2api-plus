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

func TestStepFunPlatformMigrationPreservesExistingPlatforms(t *testing.T) {
	dsn := os.Getenv("SUB2API_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("requires isolated PostgreSQL")
	}
	db, err := sql.Open("postgres", dsn)
	require.NoError(t, err)
	defer db.Close()
	ctx := context.Background()
	tx, err := db.BeginTx(ctx, nil)
	require.NoError(t, err)
	defer tx.Rollback()
	_, err = tx.ExecContext(ctx, `CREATE TEMP TABLE user_platform_quotas (platform text CHECK(platform IN ('typesafe','opencode_go'))) ON COMMIT DROP;
CREATE TEMP TABLE composite_model_routes (target_platform text CHECK(target_platform IN ('typesafe','opencode_go'))) ON COMMIT DROP;
CREATE TEMP TABLE channel_monitors (provider text CHECK(provider IN ('opencode_go'))) ON COMMIT DROP;
CREATE TEMP TABLE channel_monitor_request_templates (provider text CHECK(provider IN ('opencode_go'))) ON COMMIT DROP;
INSERT INTO user_platform_quotas VALUES ('typesafe'); INSERT INTO composite_model_routes VALUES ('typesafe');
INSERT INTO channel_monitors VALUES ('opencode_go'); INSERT INTO channel_monitor_request_templates VALUES ('opencode_go');`)
	require.NoError(t, err)
	migration, err := FS.ReadFile("277_add_stepfun_platform.sql")
	require.NoError(t, err)
	for repeat := 0; repeat < 2; repeat++ {
		_, err = tx.ExecContext(ctx, string(migration))
		require.NoError(t, err)
		for _, spec := range []struct{ table, column string }{{"user_platform_quotas", "platform"}, {"composite_model_routes", "target_platform"}, {"channel_monitors", "provider"}, {"channel_monitor_request_templates", "provider"}} {
			_, err = tx.ExecContext(ctx, "INSERT INTO "+spec.table+" ("+spec.column+") VALUES ('stepfun')")
			require.NoError(t, err)
			_, err = tx.ExecContext(ctx, "SAVEPOINT rejected_platform")
			require.NoError(t, err)
			_, err = tx.ExecContext(ctx, "INSERT INTO "+spec.table+" ("+spec.column+") VALUES ('unknown-provider')")
			require.Error(t, err)
			_, err = tx.ExecContext(ctx, "ROLLBACK TO SAVEPOINT rejected_platform")
			require.NoError(t, err)
			var existing int
			require.NoError(t, tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+spec.table+" WHERE "+spec.column+" <> 'stepfun'").Scan(&existing))
			require.Equal(t, 1, existing, "migration must retain existing provider data")
		}
	}
}
