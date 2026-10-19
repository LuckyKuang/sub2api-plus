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

// Requirement: remove retired sync storage and foreign OpenAI API-key candidates
// atomically; preserve explicit version/UA, native selections and other providers.
func TestCodexNativeIdentityManualVersionMigration(t *testing.T) {
	dsn := os.Getenv("SUB2API_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("requires isolated PostgreSQL via SUB2API_TEST_POSTGRES_DSN")
	}
	db, err := sql.Open("postgres", dsn)
	require.NoError(t, err)
	defer db.Close()
	migration, err := FS.ReadFile("290_codex_native_identity_and_manual_versions.sql")
	require.NoError(t, err)
	for _, config := range []string{`{"defaults":{"openai:apikey":"grok","openai:upstream":"claude","deepseek:apikey":"deepseek"},"profiles":{"grok":{"version":"1.0.45"}},"runtime":{"kimi":{"X-Msh-Device-Name":"ubuntu"}}}`, `{}`, `{"defaults":null}`, `not-json`} {
		t.Run(config, func(t *testing.T) {
			ctx := context.Background()
			tx, err := db.BeginTx(ctx, nil)
			require.NoError(t, err)
			defer tx.Rollback()
			_, err = tx.ExecContext(ctx, `CREATE TEMP TABLE settings(key text PRIMARY KEY,value text,updated_at timestamptz DEFAULT now()) ON COMMIT DROP;
CREATE TEMP TABLE accounts(id int,platform text,type text,credentials jsonb,updated_at timestamptz DEFAULT now()) ON COMMIT DROP;
INSERT INTO settings(key,value) VALUES
('openai_codex_client_version_synced','9.9.9'),('openai_codex_client_version_synced_checked_at','2026-10-19'),
('openai_codex_client_version_sync_error','old'),('openai_codex_version_auto_sync_enabled','true'),
('openai_codex_client_version','0.200.1'),('openai_codex_user_agent','codex_cli_rs/0.158.0 (Ubuntu 24.04; x86_64) xterm-256color'),
('claude_code_client_version_synced','2.1.281');
INSERT INTO accounts(id,platform,type,credentials) VALUES
(1,'openai','apikey','{"api_key":"retain","user_agent":"retain-UA","model_mapping":{"model":"model"},"outbound_identity":{"preset":"grok","version":"1.0.45"}}'),
(2,'openai','apikey','{"api_key":"retain","outbound_identity":{"preset":"codex"}}'),
(3,'openai','upstream','{"outbound_identity":{"preset":"grok"}}'),
(4,'grok','apikey','{"outbound_identity":{"preset":"grok"}}'),
(5,'openai','apikey','{}'),(6,'openai','apikey','null');`)
			require.NoError(t, err)
			_, err = tx.ExecContext(ctx, `INSERT INTO settings(key,value) VALUES('outbound_identity',$1)`, config)
			require.NoError(t, err)
			for range 2 {
				_, err = tx.ExecContext(ctx, string(migration))
				require.NoError(t, err)
				var count int
				require.NoError(t, tx.QueryRowContext(ctx, `SELECT count(*) FROM settings WHERE key IN ('openai_codex_client_version_synced','openai_codex_client_version_synced_checked_at','openai_codex_client_version_sync_error','openai_codex_version_auto_sync_enabled')`).Scan(&count))
				require.Zero(t, count)
				require.NoError(t, tx.QueryRowContext(ctx, `SELECT count(*) FROM settings WHERE key IN ('openai_codex_client_version','openai_codex_user_agent','claude_code_client_version_synced')`).Scan(&count))
				require.Equal(t, 3, count)
				var saved string
				require.NoError(t, tx.QueryRowContext(ctx, `SELECT value FROM settings WHERE key='outbound_identity'`).Scan(&saved))
				if len(config) > 100 {
					require.JSONEq(t, `{"defaults":{"openai:upstream":"claude","deepseek:apikey":"deepseek"},"profiles":{"grok":{"version":"1.0.45"}},"runtime":{"kimi":{"X-Msh-Device-Name":"ubuntu"}}}`, saved)
				} else {
					require.Equal(t, config, saved)
				}
				for id, want := range map[int]string{1: `{"api_key":"retain","user_agent":"retain-UA","model_mapping":{"model":"model"}}`, 2: `{"api_key":"retain","outbound_identity":{"preset":"codex"}}`, 3: `{"outbound_identity":{"preset":"grok"}}`, 4: `{"outbound_identity":{"preset":"grok"}}`, 5: `{}`, 6: `null`} {
					require.NoError(t, tx.QueryRowContext(ctx, `SELECT credentials::text FROM accounts WHERE id=$1`, id).Scan(&saved))
					require.JSONEq(t, want, saved)
				}
			}
		})
	}
	t.Run("fresh empty tables", func(t *testing.T) {
		ctx := context.Background()
		tx, err := db.BeginTx(ctx, nil)
		require.NoError(t, err)
		defer tx.Rollback()
		_, err = tx.ExecContext(ctx, `CREATE TEMP TABLE settings(key text,value text,updated_at timestamptz) ON COMMIT DROP; CREATE TEMP TABLE accounts(platform text,type text,credentials jsonb,updated_at timestamptz) ON COMMIT DROP;`)
		require.NoError(t, err)
		_, err = tx.ExecContext(ctx, string(migration))
		require.NoError(t, err)
	})
}
