//go:build integration

package migrations

import (
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"testing"

	"github.com/google/uuid"
	_ "github.com/lib/pq"
	"github.com/stretchr/testify/require"
)

func TestOfficialAgentBaselineMigrations(t *testing.T) {
	dsn := os.Getenv("SUB2API_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("requires isolated PostgreSQL via SUB2API_TEST_POSTGRES_DSN")
	}
	db, err := sql.Open("postgres", dsn)
	require.NoError(t, err)
	defer db.Close()
	for _, setting := range []string{`{"defaults":{"grok:apikey":"claude","grok:upstream":"codex"},"profiles":{"grok":{"version":"1.0.45"}}}`, `not-json`, `{"defaults":null}`} {
		t.Run(setting, func(t *testing.T) {
			ctx := context.Background()
			tx, err := db.BeginTx(ctx, nil)
			require.NoError(t, err)
			defer tx.Rollback()
			_, err = tx.ExecContext(ctx, `CREATE TEMP TABLE accounts(id int, platform text, type text, credentials jsonb, updated_at timestamptz DEFAULT now()) ON COMMIT DROP;
 CREATE TEMP TABLE settings(key text, value text, updated_at timestamptz DEFAULT now()) ON COMMIT DROP;`)
			require.NoError(t, err)
			_, err = tx.ExecContext(ctx, `INSERT INTO settings(key,value) VALUES ('outbound_identity',$1)`, setting)
			require.NoError(t, err)
			_, err = tx.ExecContext(ctx, `INSERT INTO accounts(id,platform,type,credentials) VALUES
 (1,'grok','apikey','{"api_key":"manual","outbound_identity":{"preset":"claude"}}'),
 (2,'grok','apikey','{"api_key":"native","outbound_identity":{"preset":"grok","version":"1.0.41"}}'),
 (3,'deepseek','oauth','{"access_token":"login","deepseek_harness_user_id":"bad"}'),
 (4,'deepseek','apikey','{"api_key":"manual","deepseek_harness_user_id":"11111111-1111-4111-8111-111111111111"}'),
 (5,'zhipu','oauth','{"plan_kind":"individual-coding-plan","api_key":"old-secret","access_token":"login","zhipu_project":"project","header_overrides":{"X-CODING-PLAN-API-KEY":"old-secret","keep":"yes"}}'),
 (6,'zhipu','oauth','{"plan_kind":"off-peak","api_key":"zcode-jwt","off_peak_plan_key":"old-secret","access_token":"login"}'),
 (7,'zhipu','apikey','{"plan_kind":"individual-coding-plan","api_key":"manual"}'),
 (8,'zhipu','oauth','{"plan_kind":"start-plan","api_key":"zcode-jwt"}');`)
			require.NoError(t, err)
			var anonymous string
			for repeat := 0; repeat < 2; repeat++ {
				for _, file := range []string{"291_pin_grok_native_agent.sql", "292_deepseek_harness_anonymous_user.sql", "293_zcode_project_tokens_memory_only.sql"} {
					raw, err := FS.ReadFile(file)
					require.NoError(t, err)
					_, err = tx.ExecContext(ctx, string(raw))
					require.NoError(t, err, file)
				}
				for id := 1; id <= 8; id++ {
					var raw string
					require.NoError(t, tx.QueryRowContext(ctx, `SELECT credentials::text FROM accounts WHERE id=$1`, id).Scan(&raw))
					var c map[string]any
					require.NoError(t, json.Unmarshal([]byte(raw), &c))
					switch id {
					case 1:
						require.NotContains(t, c, "outbound_identity")
						require.Equal(t, "manual", c["api_key"])
					case 2:
						require.Equal(t, map[string]any{"preset": "grok", "version": "1.0.41"}, c["outbound_identity"])
					case 3:
						user := c["deepseek_harness_user_id"].(string)
						parsed, err := uuid.Parse(user)
						require.NoError(t, err)
						require.Equal(t, uuid.Version(4), parsed.Version())
						if repeat == 0 {
							anonymous = user
						} else {
							require.Equal(t, anonymous, user)
						}
						require.Equal(t, "login", c["access_token"])
					case 4:
						require.Equal(t, "11111111-1111-4111-8111-111111111111", c["deepseek_harness_user_id"])
						require.Equal(t, "manual", c["api_key"])
					case 5:
						require.NotContains(t, c, "api_key")
						require.Equal(t, "login", c["access_token"])
						require.Equal(t, "project", c["zhipu_project"])
						require.Equal(t, map[string]any{"keep": "yes"}, c["header_overrides"])
					case 6:
						require.Equal(t, "zcode-jwt", c["api_key"])
						require.NotContains(t, c, "off_peak_plan_key")
					case 7:
						require.Equal(t, "manual", c["api_key"])
					case 8:
						require.Equal(t, "zcode-jwt", c["api_key"])
					}
				}
				var saved string
				require.NoError(t, tx.QueryRowContext(ctx, `SELECT value FROM settings`).Scan(&saved))
				if setting == `not-json` || setting == `{"defaults":null}` {
					require.Equal(t, setting, saved)
				} else {
					require.JSONEq(t, `{"defaults":{"grok:upstream":"codex"},"profiles":{"grok":{"version":"1.0.45"}}}`, saved)
				}
			}
		})
	}
}
