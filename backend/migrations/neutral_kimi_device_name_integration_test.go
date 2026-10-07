//go:build integration

package migrations

import (
	"context"
	"os"
	"testing"

	"database/sql"
	_ "github.com/lib/pq"
	"github.com/stretchr/testify/require"
)

func TestNeutralKimiDeviceNameMigration(t *testing.T) {
	dsn := os.Getenv("SUB2API_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("requires isolated PostgreSQL via SUB2API_TEST_POSTGRES_DSN")
	}
	ctx := context.Background()
	conn, err := sql.Open("postgres", dsn)
	require.NoError(t, err)
	defer conn.Close()
	migration, err := FS.ReadFile("275_neutral_kimi_device_name.sql")
	require.NoError(t, err)
	for _, setting := range []string{
		`{"runtime":{"kimi":{"X-Msh-Device-Name":"sub2api-apple","X-Msh-Os-Version":"custom-kernel","X-Msh-Device-Id":"preserve-id"}},"profiles":{"kimi":{"headers":{"x-msh-device-name":"SUB2API"}}}}`,
		`{"runtime":{"kimi":{"X-Msh-Device-Name":"custom-host"}}}`,
		`not-json`,
		`{"runtime":{"kimi":null}}`,
	} {
		t.Run(setting, func(t *testing.T) {
			tx, err := conn.BeginTx(ctx, nil)
			require.NoError(t, err)
			defer tx.Rollback()
			_, err = tx.ExecContext(ctx, `CREATE TEMP TABLE settings (key text, value text, updated_at timestamptz DEFAULT now()) ON COMMIT DROP;
    CREATE TEMP TABLE accounts (id int, credentials jsonb, updated_at timestamptz DEFAULT now()) ON COMMIT DROP;`)
			require.NoError(t, err)
			_, err = tx.ExecContext(ctx, `INSERT INTO settings (key,value) VALUES ('outbound_identity',$1)`, setting)
			require.NoError(t, err)
			_, err = tx.ExecContext(ctx, `INSERT INTO accounts (id,credentials) VALUES
    (1,'{"token":"keep","outbound_identity":{"preset":"kimi","headers":{"X-Msh-Device-Name":"sub2api-apple","X-Msh-Device-Id":"keep-id"}}}'),
    (2,'{"outbound_identity":{"preset":"kimi","headers":{"X-Msh-Device-Name":"my-workstation"}}}'),
    (3,'{"outbound_identity":{"preset":"zcode","headers":{"X-Msh-Device-Name":"sub2api-apple"}}}'),
    (4,'{}');`)
			require.NoError(t, err)
			for repeat := 0; repeat < 2; repeat++ {
				_, err = tx.ExecContext(ctx, string(migration))
				require.NoError(t, err)
				var name, uuid, token string
				require.NoError(t, tx.QueryRowContext(ctx, `SELECT credentials #>> '{outbound_identity,headers,X-Msh-Device-Name}', credentials #>> '{outbound_identity,headers,X-Msh-Device-Id}', credentials ->> 'token' FROM accounts WHERE id=1`).Scan(&name, &uuid, &token))
				require.Equal(t, "ubuntu", name)
				require.Equal(t, "keep-id", uuid)
				require.Equal(t, "keep", token)
				require.NoError(t, tx.QueryRowContext(ctx, `SELECT credentials #>> '{outbound_identity,headers,X-Msh-Device-Name}' FROM accounts WHERE id=2`).Scan(&name))
				require.Equal(t, "my-workstation", name)
				require.NoError(t, tx.QueryRowContext(ctx, `SELECT credentials #>> '{outbound_identity,headers,X-Msh-Device-Name}' FROM accounts WHERE id=3`).Scan(&name))
				require.Equal(t, "sub2api-apple", name)
				var saved string
				require.NoError(t, tx.QueryRowContext(ctx, `SELECT value FROM settings WHERE key='outbound_identity'`).Scan(&saved))
				if setting[0] == '{' && setting != `{"runtime":{"kimi":null}}` && setting != `{"runtime":{"kimi":{"X-Msh-Device-Name":"custom-host"}}}` {
					require.JSONEq(t, `{"runtime":{"kimi":{"X-Msh-Device-Name":"ubuntu","X-Msh-Os-Version":"custom-kernel","X-Msh-Device-Id":"preserve-id"}},"profiles":{"kimi":{"headers":{"x-msh-device-name":"ubuntu"}}}}`, saved)
				} else {
					require.Equal(t, setting, saved)
				}
			}
		})
	}
}
