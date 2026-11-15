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

// R4 forbids guessing a historical platform from public aliases: aliases can be
// mapped across platforms. Only submissions recording an explicit resolved
// platform after migration 311 retain that evidence.
func TestAsyncImageLegacyPlatformRepairPreservesExplicitSubmissions(t *testing.T) {
	dsn := os.Getenv("SUB2API_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("requires isolated PostgreSQL")
	}
	db, err := sql.Open("postgres", dsn)
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	ctx := context.Background()
	tx, err := db.BeginTx(ctx, nil)
	require.NoError(t, err)
	defer func() { _ = tx.Rollback() }()
	_, err = tx.ExecContext(ctx, `CREATE TEMP TABLE schema_migrations(filename text,applied_at timestamptz) ON COMMIT DROP;
 CREATE TEMP TABLE async_image_tasks(task_id text,created_at timestamptz,platform text,model text) ON COMMIT DROP;
 INSERT INTO schema_migrations VALUES ('311_async_image_task_user_history.sql','2026-11-13T12:00:00Z');
 INSERT INTO async_image_tasks VALUES
 ('old-gpt','2026-11-12T12:00:00Z','openai','gpt-custom-alias'),
 ('old-grok','2026-11-12T12:00:00Z','grok','grok-custom-alias'),
 ('new-explicit','2026-11-14T12:00:00Z','grok','gpt-image-2'),
 ('old-unknown','2026-11-12T12:00:00Z','','unknown');`)
	require.NoError(t, err)
	migration, err := FS.ReadFile("313_async_image_task_legacy_platform.sql")
	require.NoError(t, err)
	for attempt := 0; attempt < 2; attempt++ {
		_, err = tx.ExecContext(ctx, string(migration))
		require.NoError(t, err)
		for id, want := range map[string]string{"old-gpt": "", "old-grok": "", "new-explicit": "grok", "old-unknown": ""} {
			var got string
			require.NoError(t, tx.QueryRowContext(ctx, "SELECT platform FROM async_image_tasks WHERE task_id=$1", id).Scan(&got))
			require.Equal(t, want, got, id)
		}
	}
}
