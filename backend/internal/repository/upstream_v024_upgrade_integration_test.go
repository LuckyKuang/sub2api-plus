//go:build integration

package repository

import (
	"context"
	"database/sql"
	"fmt"
	"io/fs"
	"net/url"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	dbmigrations "github.com/LuckyKuang/sub2api-plus/migrations"
	"github.com/stretchr/testify/require"
)

func TestV024UpgradePreservesV021PlusData(t *testing.T) {
	// Migrations through 258 are the immutable v0.2.1+custom.003 baseline.
	// Use a fresh database because repair migrations explicitly address public.
	ctx := context.Background()
	dbName := fmt.Sprintf("sub2api_upgrade_%d", time.Now().UnixNano())
	_, err := integrationDB.ExecContext(ctx, "CREATE DATABASE "+dbName)
	require.NoError(t, err)
	t.Cleanup(func() {
		_, err := integrationDB.ExecContext(ctx, "DROP DATABASE "+dbName+" WITH (FORCE)")
		require.NoError(t, err)
	})
	dsn := integrationPostgresDSN
	if strings.HasPrefix(dsn, "postgres://") || strings.HasPrefix(dsn, "postgresql://") {
		parsed, err := url.Parse(dsn)
		require.NoError(t, err)
		parsed.Path = "/" + dbName
		dsn = parsed.String()
	} else {
		dsn += " dbname=" + dbName
	}
	db, err := sql.Open("postgres", dsn)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, db.Close()) })

	names, err := fs.Glob(dbmigrations.FS, "*.sql")
	require.NoError(t, err)
	baseline := fstest.MapFS{}
	for _, name := range names {
		if name < "259_" {
			body, err := dbmigrations.FS.ReadFile(name)
			require.NoError(t, err)
			baseline[name] = &fstest.MapFile{Data: body}
		}
	}
	require.NoError(t, applyMigrationsFS(ctx, db, baseline))
	_, err = db.ExecContext(ctx, `
INSERT INTO users (id,email,password_hash,balance) VALUES (1,'upgrade@example.test','fixture',12.345);
INSERT INTO accounts (id,name,platform,type,credentials,extra) VALUES
 (1,'quota source','openai','oauth','{"user_agent":"codex_cli_rs/0.125.0 (Linux; x86_64) test"}','{"weekly_usage":12}');
INSERT INTO groups (id,name,platform,subscription_type,models_list_config,quota_reset_source_account_id,
 quota_reset_source_account_name,quota_reset_source_reset_at,quota_reset_config_version,quota_reset_include_monthly)
 VALUES (101,'Plus subscription','openai','subscription','{"enabled":true,"models":[" GPT-5.6 ","gpt-5.6","claude-*"]}',
 1,'quota source','2026-09-10T00:00:00Z',7,true),
 (102,'Legacy empty display','openai','standard','{"enabled":true,"models":[]}',NULL,'',NULL,0,false);
INSERT INTO account_groups (account_id,group_id) VALUES (1,101);
INSERT INTO user_subscriptions (user_id,group_id,starts_at,expires_at,five_hour_usage_usd,daily_usage_usd,
 weekly_usage_usd,monthly_usage_usd,quota_follow_reset_event_id)
 VALUES (1,101,'2026-09-01','2026-10-01',1.25,2.5,11,33,4);
INSERT INTO openai_oauth_weekly_reset_observations (account_id,reset_at,observed_at)
 VALUES (1,'2026-09-17','2026-09-10');
INSERT INTO group_quota_follow_reset_events (group_id,source_account_id,config_version,upstream_reset_at,effective_at)
 VALUES (101,1,7,'2026-09-17','2026-09-10');
INSERT INTO user_platform_quotas (user_id,platform,weekly_limit_usd,weekly_usage_usd) VALUES (1,'openai',100,11);
INSERT INTO settings (key,value) VALUES ('ops_runtime_log_config','{"level":"warn","retention_days":45}'),
 ('risk_control_enabled','true'),('openai_codex_user_agent','fixture-global-identity')
 ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value;
`)
	require.NoError(t, err)
	snapshot := func(query string) string {
		t.Helper()
		var value string
		require.NoError(t, db.QueryRowContext(ctx, query).Scan(&value))
		return value
	}
	queries := []string{
		`SELECT jsonb_agg(to_jsonb(t) ORDER BY id)::text FROM users t`,
		`SELECT jsonb_agg(to_jsonb(t) ORDER BY id)::text FROM accounts t`,
		`SELECT jsonb_agg(to_jsonb(t) ORDER BY group_id)::text FROM account_groups t`,
		`SELECT jsonb_agg(to_jsonb(t) ORDER BY id)::text FROM user_subscriptions t`,
		`SELECT jsonb_agg(to_jsonb(t) - ARRAY['used_percent','reset_sequence','pending_reset_at','pending_observed_at','next_poll_at'] ORDER BY account_id)::text FROM openai_oauth_weekly_reset_observations t`,
		`SELECT jsonb_agg(to_jsonb(t) - 'reset_sequence' ORDER BY id)::text FROM group_quota_follow_reset_events t`,
		`SELECT jsonb_agg(to_jsonb(t) ORDER BY id)::text FROM user_platform_quotas t`,
		`SELECT jsonb_agg(to_jsonb(t) ORDER BY key)::text FROM settings t WHERE key <> 'ops_runtime_log_config'`,
		`SELECT jsonb_agg(to_jsonb(t) ORDER BY filename)::text FROM schema_migrations t WHERE filename < '259_'`,
	}
	before := make([]string, len(queries))
	for i, query := range queries {
		before[i] = snapshot(query)
	}
	groupsBefore := snapshot(`SELECT jsonb_agg(to_jsonb(t) - 'models_list_config' ORDER BY id)::text FROM groups t`)
	require.NoError(t, ApplyMigrations(ctx, db))
	// A second startup must preserve checksums and data without replaying policy.
	require.NoError(t, ApplyMigrations(ctx, db))
	for i, query := range queries {
		require.JSONEq(t, before[i], snapshot(query), query)
	}
	// New observation metadata must initialize without manufacturing a reset
	// or changing any of the old columns protected by the snapshots above.
	require.JSONEq(t, `{"sequence":0,"used":null,"pending":null,"pending_at":null,"poll_scheduled":true}`, snapshot(`
		SELECT jsonb_build_object('sequence', reset_sequence, 'used', used_percent,
		    'pending', pending_reset_at, 'pending_at', pending_observed_at,
		    'poll_scheduled', next_poll_at IS NOT NULL)::text
		FROM openai_oauth_weekly_reset_observations WHERE account_id=1`))
	require.Equal(t, "0", snapshot(`SELECT reset_sequence::text FROM group_quota_follow_reset_events WHERE group_id=101`))
	require.JSONEq(t, groupsBefore, snapshot(`SELECT jsonb_agg(to_jsonb(t) - 'model_allowlist' ORDER BY id)::text FROM groups t`))
	require.JSONEq(t, `{"enabled":true,"models":["GPT-5.6","claude-*"]}`, snapshot(`SELECT model_allowlist::text FROM groups WHERE id=101`))
	require.JSONEq(t, `{"enabled":false,"models":[]}`, snapshot(`SELECT model_allowlist::text FROM groups WHERE id=102`))
	require.JSONEq(t, `{"level":"warn","retention_days":45,"persist_access_logs":true}`, snapshot(`SELECT value FROM settings WHERE key='ops_runtime_log_config'`))
	_, err = db.ExecContext(ctx, `INSERT INTO user_platform_quotas (user_id,platform) VALUES (1,'minimax')`)
	require.NoError(t, err, "MiniMax must be accepted without losing existing Plus platform quotas")
}
