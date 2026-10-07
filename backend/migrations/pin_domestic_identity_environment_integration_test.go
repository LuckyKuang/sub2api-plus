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

func TestPinDomesticIdentityEnvironmentMigration(t *testing.T) {
	dsn := os.Getenv("SUB2API_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("requires isolated PostgreSQL via SUB2API_TEST_POSTGRES_DSN")
	}
	db, err := sql.Open("postgres", dsn)
	require.NoError(t, err)
	defer db.Close()
	migration, err := FS.ReadFile("276_pin_domestic_identity_environment.sql")
	require.NoError(t, err)
	for _, tc := range []struct{ name, before, after string }{
		{"all tiers", `{"runtime":{"kimi":{"x-msh-device-model":"host","X-Msh-Os-Version":"old","X-Msh-Device-Name":"ubuntu","X-Msh-Device-Id":"keep"},"zcode":{"X-Platform":"darwin-arm64","X-Os-Category":"macos","X-Os-Version":"old","X-Client-Language":"zh-CN","X-Client-Timezone":"Asia/Shanghai"}},"profiles":{"kimi":{"preset":"kimi","version":"2.1.1","headers":{"X-Msh-Device-Model":"host","X-Msh-Os-Version":"old"}},"zcode":{"headers":{"X-Platform":"host","x-os-category":"host","x-os-version":"old","X-Client-Language":"en-US"}},"deepseek":{"preset":"deepseek","language":"en-US"}}}`, `{"runtime":{"kimi":{"X-Msh-Device-Name":"ubuntu","X-Msh-Device-Id":"keep"},"zcode":{"X-Client-Language":"zh-CN","X-Client-Timezone":"Asia/Shanghai"}},"profiles":{"kimi":{"preset":"kimi","version":"2.1.1","headers":{}},"zcode":{"headers":{"X-Client-Language":"en-US"}},"deepseek":{"preset":"deepseek","language":"en-US"}}}`},
		{"empty", `{}`, `{}`},
		{"null fields", `{"runtime":{"kimi":null},"profiles":{"zcode":{"headers":null}}}`, `{"runtime":{"kimi":null},"profiles":{"zcode":{"headers":null}}}`},
		{"malformed", `not-json`, `not-json`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			tx, err := db.BeginTx(ctx, nil)
			require.NoError(t, err)
			defer tx.Rollback()
			_, err = tx.ExecContext(ctx, `CREATE TEMP TABLE settings (key text, value text, updated_at timestamptz DEFAULT now()) ON COMMIT DROP;
CREATE TEMP TABLE accounts (id int, credentials jsonb, updated_at timestamptz DEFAULT now()) ON COMMIT DROP;`)
			require.NoError(t, err)
			_, err = tx.ExecContext(ctx, `INSERT INTO settings (key,value) VALUES ('outbound_identity',$1)`, tc.before)
			require.NoError(t, err)
			_, err = tx.ExecContext(ctx, `INSERT INTO accounts (id,credentials) VALUES
(1,'{"token":"keep","outbound_identity":{"preset":"kimi","headers":{"X-Msh-Device-Model":"host","x-msh-os-version":"old","X-Msh-Device-Name":"custom","X-Msh-Device-Id":"keep"}}}'),
(2,'{"outbound_identity":{"preset":"zcode","headers":{"x-platform":"host","X-Os-Category":"macos","X-Os-Version":"old","X-Client-Timezone":"Asia/Shanghai"}}}'),
(3,'{"outbound_identity":{"preset":"claude","headers":{"X-Os-Version":"untouched"}}}'),
(4,'{}'), (5,'{"outbound_identity":{"preset":"kimi"}}');`)
			require.NoError(t, err)
			for repeat := 0; repeat < 2; repeat++ {
				_, err = tx.ExecContext(ctx, string(migration))
				require.NoError(t, err)
				var saved string
				require.NoError(t, tx.QueryRowContext(ctx, `SELECT value FROM settings WHERE key='outbound_identity'`).Scan(&saved))
				if tc.name == "malformed" {
					require.Equal(t, tc.after, saved)
				} else {
					require.JSONEq(t, tc.after, saved)
				}
				for id, expected := range []string{
					`{"token":"keep","outbound_identity":{"preset":"kimi","headers":{"X-Msh-Device-Name":"custom","X-Msh-Device-Id":"keep"}}}`,
					`{"outbound_identity":{"preset":"zcode","headers":{"X-Client-Timezone":"Asia/Shanghai"}}}`,
					`{"outbound_identity":{"preset":"claude","headers":{"X-Os-Version":"untouched"}}}`, `{}`, `{"outbound_identity":{"preset":"kimi"}}`,
				} {
					require.NoError(t, tx.QueryRowContext(ctx, `SELECT credentials::text FROM accounts WHERE id=$1`, id+1).Scan(&saved))
					require.JSONEq(t, expected, saved)
				}
			}
		})
	}
}
