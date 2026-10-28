//go:build integration

package migrations

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"testing"
	"time"

	_ "github.com/lib/pq"
	"github.com/stretchr/testify/require"
)

// Existing groups must remain opt-out; replay must preserve key audit history
// and an administrator's explicitly enabled image-count billing policy.
func TestScopedAdminAndImageBillingMigrationsFreshUpgradeAndReplay(t *testing.T) {
	dsn := os.Getenv("SUB2API_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("requires isolated PostgreSQL via SUB2API_TEST_POSTGRES_DSN")
	}
	db, err := sql.Open("postgres", dsn)
	require.NoError(t, err)
	defer db.Close()
	for _, upgrade := range []bool{false, true} {
		t.Run(fmt.Sprintf("upgrade=%t", upgrade), func(t *testing.T) {
			tx, err := db.BeginTx(context.Background(), nil)
			require.NoError(t, err)
			defer tx.Rollback()
			schema := fmt.Sprintf("migration_302_303_%d", time.Now().UnixNano())
			_, err = tx.Exec(`CREATE SCHEMA ` + schema + `; SET LOCAL search_path TO ` + schema)
			require.NoError(t, err)
			_, err = tx.Exec(`CREATE TABLE users(id bigint PRIMARY KEY); INSERT INTO users VALUES(9);
CREATE TABLE groups(id bigint PRIMARY KEY, name text NOT NULL); INSERT INTO groups VALUES(1,'historical');`)
			require.NoError(t, err)
			keys, err := FS.ReadFile("302_scoped_admin_api_keys.sql")
			require.NoError(t, err)
			images, err := FS.ReadFile("303_group_image_count_billing.sql")
			require.NoError(t, err)
			if upgrade {
				// Represent the pre-existing key table without the new soft-delete column.
				_, err = tx.Exec(string(keys))
				require.NoError(t, err)
				_, err = tx.Exec(`ALTER TABLE scoped_admin_api_keys DROP COLUMN deleted_at`)
				require.NoError(t, err)
				_, err = tx.Exec(`INSERT INTO scoped_admin_api_keys(id,name,key_prefix,key_hash,created_by,revoked_at)
VALUES('before-upgrade','old key','admin_','old-irreversible-hash',9,'2026-01-01')`)
				require.NoError(t, err)
			}
			_, err = tx.Exec(string(keys) + "\n" + string(images))
			require.NoError(t, err)
			if upgrade {
				var historical bool
				require.NoError(t, tx.QueryRow(`SELECT key_hash='old-irreversible-hash' AND revoked_at IS NOT NULL AND deleted_at IS NULL
FROM scoped_admin_api_keys WHERE id='before-upgrade'`).Scan(&historical))
				require.True(t, historical)
			}
			var enabled bool
			require.NoError(t, tx.QueryRow(`SELECT image_count_billing FROM groups WHERE id=1`).Scan(&enabled))
			require.False(t, enabled)
			_, err = tx.Exec(`INSERT INTO scoped_admin_api_keys(id,name,key_prefix,key_hash,created_by,revoked_at,deleted_at)
VALUES('history','historical key','admin_','irreversible-hash',9,'2026-01-01','2026-01-02');
UPDATE groups SET image_count_billing=true WHERE id=1;`)
			require.NoError(t, err)
			_, err = tx.Exec(string(keys) + "\n" + string(images))
			require.NoError(t, err)
			var hash string
			var retained bool
			require.NoError(t, tx.QueryRow(`SELECT key_hash,revoked_at IS NOT NULL AND deleted_at IS NOT NULL FROM scoped_admin_api_keys WHERE id='history'`).Scan(&hash, &retained))
			require.Equal(t, "irreversible-hash", hash)
			require.True(t, retained)
			require.NoError(t, tx.QueryRow(`SELECT image_count_billing FROM groups WHERE id=1`).Scan(&enabled))
			require.True(t, enabled)
			_, err = tx.Exec(`INSERT INTO groups(id,name) VALUES(2,'new')`)
			require.NoError(t, err)
			require.NoError(t, tx.QueryRow(`SELECT image_count_billing FROM groups WHERE id=2`).Scan(&enabled))
			require.False(t, enabled)
		})
	}
}
