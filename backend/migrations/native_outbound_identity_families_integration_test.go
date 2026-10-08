//go:build integration

package migrations

import (
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"testing"

	_ "github.com/lib/pq"
	"github.com/stretchr/testify/require"
)

func TestNativeOutboundIdentityFamilyMigration(t *testing.T) {
	dsn := os.Getenv("SUB2API_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("requires isolated PostgreSQL via SUB2API_TEST_POSTGRES_DSN")
	}
	db, err := sql.Open("postgres", dsn)
	require.NoError(t, err)
	defer db.Close()
	migration, err := FS.ReadFile("278_pin_native_outbound_identity_families.sql")
	require.NoError(t, err)
	candidate := func(preset string) map[string]any {
		value := map[string]any{"preset": preset}
		switch preset {
		case "deepseek":
			value["language"] = "en-US"
			value["timezone"] = "Asia/Shanghai"
		case "kimi":
			value["headers"] = map[string]any{"X-Msh-Device-Name": "ubuntu", "X-Msh-Device-Id": "11111111-1111-4111-8111-111111111111"}
		case "minimax", "minimax_apikey":
			value["timezone"] = "UTC"
		case "zcode":
			value["headers"] = map[string]any{"X-Client-Language": "zh-CN", "X-Client-Timezone": "Europe/Amsterdam"}
		case "stepfun":
		default:
			value["version"] = "3.9.1"
		}
		return value
	}
	for _, before := range []string{
		`{"defaults":{"deepseek:apikey":"codex","kimi:apikey":"kimi","minimax:apikey":"minimax","zhipu:apikey":"claude","stepfun:apikey":"grok","anthropic:bedrock":"codex","anthropic:service_account":"grok","gemini:service_account":"claude","anthropic:oauth":"claude","openai:apikey":"claude","opencode_go:apikey":"stepfun","typesafe:apikey":"grok","unknown:apikey":"codex"},"profiles":{"kimi":{"version":"2.2.0"}},"runtime":{"kimi":{"X-Msh-Device-Name":"ubuntu"}}}`,
		`{}`, `{"defaults":null}`, `not-json`,
	} {
		t.Run(before, func(t *testing.T) {
			ctx := context.Background()
			tx, err := db.BeginTx(ctx, nil)
			require.NoError(t, err)
			defer tx.Rollback()
			_, err = tx.ExecContext(ctx, `CREATE TEMP TABLE settings(key text, value text, updated_at timestamptz DEFAULT now()) ON COMMIT DROP;
CREATE TEMP TABLE accounts(id int, platform text, type text, credentials jsonb, extra jsonb, updated_at timestamptz DEFAULT now()) ON COMMIT DROP;`)
			require.NoError(t, err)
			_, err = tx.ExecContext(ctx, `INSERT INTO settings(key,value) VALUES ('outbound_identity',$1)`, before)
			require.NoError(t, err)
			cases := []struct{ platform, kind, preset string }{
				{"deepseek", "apikey", "deepseek"}, {"kimi", "apikey", "kimi"}, {"minimax", "apikey", "minimax_apikey"}, {"minimax", "oauth", "minimax"}, {"zhipu", "apikey", "zcode"}, {"stepfun", "apikey", "stepfun"},
				{"anthropic", "bedrock", "claude"}, {"anthropic", "service_account", "claude"}, {"gemini", "service_account", "gemini"},
				{"deepseek", "oauth", "deepseek"}, {"kimi", "oauth", "kimi"}, {"zhipu", "oauth", "zcode"}, {"stepfun", "oauth", "stepfun"},
			}
			for id, tc := range cases {
				for variant, preset := range []string{"grok", tc.preset} {
					credentials, err := json.Marshal(map[string]any{"api_key": "retain-key", "access_token": "retain-token", "model_mapping": map[string]string{"model": "model"}, "outbound_identity": candidate(preset)})
					require.NoError(t, err)
					_, err = tx.ExecContext(ctx, `INSERT INTO accounts(id,platform,type,credentials,extra) VALUES($1,$2,$3,$4,'{"rate_multiplier":2}')`, id*2+variant, tc.platform, tc.kind, string(credentials))
					require.NoError(t, err)
				}
			}
			_, err = tx.ExecContext(ctx, `INSERT INTO accounts(id,platform,type,credentials) VALUES
(100,'openai','apikey','{"outbound_identity":{"preset":"grok"}}'),
(101,'opencode_go','apikey','{"outbound_identity":{"preset":"claude"}}'),
(102,'kimi','apikey','{}');`)
			require.NoError(t, err)
			for repeat := 0; repeat < 2; repeat++ {
				_, err = tx.ExecContext(ctx, string(migration))
				require.NoError(t, err)
				var saved string
				require.NoError(t, tx.QueryRowContext(ctx, `SELECT value FROM settings`).Scan(&saved))
				if before[0] == '{' && len(before) > 50 {
					require.JSONEq(t, `{"defaults":{"openai:apikey":"claude","opencode_go:apikey":"stepfun","typesafe:apikey":"grok"},"profiles":{"kimi":{"version":"2.2.0"}},"runtime":{"kimi":{"X-Msh-Device-Name":"ubuntu"}}}`, saved)
				} else {
					require.Equal(t, before, saved)
				}
				for id, tc := range cases {
					for variant := 0; variant < 2; variant++ {
						var credentials, extra string
						require.NoError(t, tx.QueryRowContext(ctx, `SELECT credentials::text, extra::text FROM accounts WHERE id=$1`, id*2+variant).Scan(&credentials, &extra))
						var fields map[string]any
						require.NoError(t, json.Unmarshal([]byte(credentials), &fields))
						require.Equal(t, "retain-key", fields["api_key"])
						require.Equal(t, "retain-token", fields["access_token"])
						require.Equal(t, map[string]any{"model": "model"}, fields["model_mapping"])
						require.JSONEq(t, `{"rate_multiplier":2}`, extra)
						if variant == 0 {
							require.NotContains(t, fields, "outbound_identity")
						} else {
							require.Equal(t, candidate(tc.preset), fields["outbound_identity"])
						}
					}
				}
				for id, expected := range map[int]string{100: `{"outbound_identity":{"preset":"grok"}}`, 101: `{"outbound_identity":{"preset":"claude"}}`, 102: `{}`} {
					require.NoError(t, tx.QueryRowContext(ctx, `SELECT credentials::text FROM accounts WHERE id=$1`, id).Scan(&saved))
					require.JSONEq(t, expected, saved)
				}
			}
		})
	}
}
