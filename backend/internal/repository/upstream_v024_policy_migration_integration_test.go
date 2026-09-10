//go:build integration

package repository

import (
	"context"
	"database/sql"
	"testing"

	dbmigrations "github.com/LuckyKuang/sub2api-plus/migrations"
	"github.com/stretchr/testify/require"
)

func runV024PolicyMigration(t *testing.T, tx *sql.Tx, name string) error {
	t.Helper()
	body, err := dbmigrations.FS.ReadFile(name)
	require.NoError(t, err)
	_, err = tx.ExecContext(context.Background(), string(body))
	return err
}

func TestMigration262NormalizesLegacyAllowlist(t *testing.T) {
	cases := []struct {
		name, input, expected string
		invalid               bool
	}{
		{"empty enabled", `{"enabled":true,"models":[]}`, `{"enabled":false,"models":[]}`, false},
		{"missing models", `{"enabled":true}`, `{"enabled":false,"models":[]}`, false},
		{"null models", `{"enabled":true,"models":null}`, `{"enabled":false,"models":[]}`, false},
		{"blank", `{"enabled":true,"models":[" ","\t"]}`, `{"enabled":false,"models":[]}`, false},
		{"normalize", `{"enabled":true,"models":[" GPT-5.6 ","gpt-5.6","claude-*"]}`, `{"enabled":true,"models":["GPT-5.6","claude-*"]}`, false},
		{"disabled", `{"enabled":false,"models":["gpt-5.6"]}`, `{"enabled":false,"models":["gpt-5.6"]}`, false},
		{"default", `{}`, `{}`, false},
		{"invalid wildcard", `{"enabled":true,"models":["gpt*5"]}`, "", true},
		{"invalid entry", `{"enabled":true,"models":[42]}`, "", true},
		{"invalid list", `{"enabled":true,"models":"gpt"}`, "", true},
		{"invalid enabled", `{"enabled":"true"}`, "", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tx := testTx(t)
			_, err := tx.Exec(`CREATE TEMP TABLE groups (id BIGINT, model_allowlist JSONB) ON COMMIT DROP`)
			require.NoError(t, err)
			_, err = tx.Exec(`INSERT INTO groups VALUES (7, $1::jsonb)`, tc.input)
			require.NoError(t, err)
			err = runV024PolicyMigration(t, tx, "262_normalize_legacy_model_allowlist.sql")
			if tc.invalid {
				require.ErrorContains(t, err, "group 7")
				return
			}
			require.NoError(t, err)
			var actual string
			require.NoError(t, tx.QueryRow(`SELECT model_allowlist::text FROM groups`).Scan(&actual))
			require.JSONEq(t, tc.expected, actual)
		})
	}
}

func TestMigration263AccessLogsNewAndExistingInstallations(t *testing.T) {
	cases := []struct {
		name            string
		existing        bool
		input, expected string
	}{
		{"fresh", false, "", ""},
		{"existing missing config", true, "", `{"persist_access_logs":true}`},
		{"existing preserves other fields", true, `{"level":"warn"}`, `{"level":"warn","persist_access_logs":true}`},
		{"explicit false", true, `{"persist_access_logs":false}`, `{"persist_access_logs":false}`},
		{"explicit true", true, `{"persist_access_logs":true}`, `{"persist_access_logs":true}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tx := testTx(t)
			_, err := tx.Exec(`CREATE TEMP TABLE users (id BIGINT) ON COMMIT DROP;
            CREATE TEMP TABLE settings (key TEXT PRIMARY KEY, value TEXT NOT NULL, updated_at TIMESTAMPTZ) ON COMMIT DROP`)
			require.NoError(t, err)
			if tc.existing {
				_, err = tx.Exec(`INSERT INTO users VALUES (1)`)
				require.NoError(t, err)
			}
			if tc.input != "" {
				_, err = tx.Exec(`INSERT INTO settings (key,value) VALUES ('ops_runtime_log_config',$1)`, tc.input)
				require.NoError(t, err)
			}
			for i := 0; i < 2; i++ {
				require.NoError(t, runV024PolicyMigration(t, tx, "263_preserve_existing_access_log_persistence.sql"))
				var actual string
				err = tx.QueryRow(`SELECT value FROM settings WHERE key='ops_runtime_log_config'`).Scan(&actual)
				if tc.expected == "" {
					require.ErrorIs(t, err, sql.ErrNoRows)
				} else {
					require.NoError(t, err)
					require.JSONEq(t, tc.expected, actual)
				}
			}
		})
	}
}
