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

// Official v0.2.18 upgrade contract: existing Gemini/Antigravity permissions
// become enabled; composite/other groups and every Plus field stay unchanged.
func TestGeminiImagePermissionUpgradePreservesOtherGroupsAndPlusFields(t *testing.T) {
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
		id bigint PRIMARY KEY, platform text, allow_image_generation boolean,
		five_hour_limit_usd numeric, model_pricing jsonb) ON COMMIT DROP;
		INSERT INTO groups VALUES
		(1,'gemini',false,12.50,'{"custom":0}'),
		(2,'antigravity',false,12.50,'{"custom":0}'),
		(3,'composite',false,12.50,'{"custom":0}'),
		(4,'openai',false,12.50,'{"custom":0}'),
		(5,'gemini',true,12.50,'{"custom":0}'),
		(6,'composite',true,12.50,'{"custom":0}');`)
	require.NoError(t, err)
	migration, err := FS.ReadFile("295_enable_gemini_image_generation_groups.sql")
	require.NoError(t, err)
	for attempt := 0; attempt < 2; attempt++ {
		_, err = tx.ExecContext(ctx, string(migration))
		require.NoError(t, err)
		for id, expected := range map[int]bool{1: true, 2: true, 3: false, 4: false, 5: true, 6: true} {
			var enabled bool
			var limit float64
			var pricing string
			require.NoError(t, tx.QueryRowContext(ctx, `SELECT allow_image_generation,
				five_hour_limit_usd,model_pricing::text FROM groups WHERE id=$1`, id).
				Scan(&enabled, &limit, &pricing))
			require.Equal(t, expected, enabled, "group %d, attempt %d", id, attempt)
			require.Equal(t, 12.50, limit)
			require.JSONEq(t, `{"custom":0}`, pricing)
		}
	}
}
