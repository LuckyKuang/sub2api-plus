//go:build integration

package repository

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"fmt"
	"net/url"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/LuckyKuang/sub2api-plus/migrations"
	"github.com/google/uuid"
	"github.com/lib/pq"
	"github.com/stretchr/testify/require"
)

// An interrupted concurrent build leaves an INVALID index with the final name.
// The actual embedded 309 must repair it; a synthetic filename derived from the
// runner constant would conceal an upstream-number/Plus-number mismatch.
func TestUpstream021MigrationsRepairInvalidIndexAndPreserveTTL(t *testing.T) {
	ctx := context.Background()
	name := "upstream021_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	_, err := integrationDB.ExecContext(ctx, "CREATE DATABASE "+pq.QuoteIdentifier(name))
	require.NoError(t, err)
	t.Cleanup(func() {
		_, err := integrationDB.ExecContext(ctx, "DROP DATABASE "+pq.QuoteIdentifier(name)+" WITH (FORCE)")
		require.NoError(t, err)
	})
	dsn := integrationPostgresDSN
	if strings.HasPrefix(dsn, "postgres://") || strings.HasPrefix(dsn, "postgresql://") {
		u, err := url.Parse(dsn)
		require.NoError(t, err)
		u.Path = "/" + name
		dsn = u.String()
	} else {
		dsn += " dbname=" + name
	}
	db, err := sql.Open("postgres", dsn)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	_, err = db.ExecContext(ctx, `CREATE TABLE usage_logs (api_key_id bigint, created_at timestamptz, actual_cost numeric);
INSERT INTO usage_logs VALUES (7, '2026-11-12T00:00:00Z', 0.25), (7, '2026-11-12T00:00:00Z', 0.75);
CREATE TABLE channel_model_pricing (id bigint);
CREATE TABLE channel_pricing_intervals (id bigint);
CREATE TABLE channel_account_stats_model_pricing (id bigint);
CREATE TABLE channel_account_stats_pricing_intervals (id bigint);`)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `CREATE UNIQUE INDEX CONCURRENTLY idx_usage_logs_api_key_created_cost ON usage_logs (api_key_id, created_at)`)
	require.Error(t, err, "the duplicate fixture must leave a failed concurrent build")
	var valid bool
	require.NoError(t, db.QueryRowContext(ctx, `SELECT indisvalid FROM pg_index WHERE indexrelid='idx_usage_logs_api_key_created_cost'::regclass`).Scan(&valid))
	require.False(t, valid)
	fsys := fstest.MapFS{}
	for _, filename := range []string{"309_usage_log_api_key_cost_index_notx.sql", "310_openai_cache_write_ttl.sql"} {
		body, err := migrations.FS.ReadFile(filename)
		require.NoError(t, err)
		fsys[filename] = &fstest.MapFile{Data: body}
	}
	require.NoError(t, applyMigrationsFS(ctx, db, fsys))
	var definition string
	require.NoError(t, db.QueryRowContext(ctx, `SELECT indisvalid, pg_get_indexdef(indexrelid) FROM pg_index WHERE indexrelid='idx_usage_logs_api_key_created_cost'::regclass`).Scan(&valid, &definition))
	require.True(t, valid)
	require.Contains(t, definition, "USING btree (api_key_id, created_at) INCLUDE (actual_cost)")
	require.NotContains(t, definition, "UNIQUE")
	_, err = db.ExecContext(ctx, `UPDATE usage_logs SET cache_creation_30m_tokens=9, cache_creation_12h_tokens=2390;
INSERT INTO channel_model_pricing (id, cache_write_12h_price) VALUES (1, NULL), (2, 0), (3, 0.000001625)`)
	require.NoError(t, err)
	// Repeat both raw DDL (IF NOT EXISTS) and the startup runner/checksum path.
	_, err = db.ExecContext(ctx, string(fsys["310_openai_cache_write_ttl.sql"].Data))
	require.NoError(t, err)
	require.NoError(t, applyMigrationsFS(ctx, db, fsys))
	var short, long int
	var cost float64
	require.NoError(t, db.QueryRowContext(ctx, `SELECT cache_creation_30m_tokens,cache_creation_12h_tokens,actual_cost FROM usage_logs ORDER BY actual_cost LIMIT 1`).Scan(&short, &long, &cost))
	require.Equal(t, 9, short)
	require.Equal(t, 2390, long)
	require.Equal(t, 0.25, cost)
	var prices string
	require.NoError(t, db.QueryRowContext(ctx, `SELECT json_agg(cache_write_12h_price ORDER BY id)::text FROM channel_model_pricing`).Scan(&prices))
	require.JSONEq(t, `[null,0,0.000001625]`, prices)
	for filename, file := range fsys {
		var checksum string
		require.NoError(t, db.QueryRowContext(ctx, `SELECT checksum FROM schema_migrations WHERE filename=$1`, filename).Scan(&checksum))
		require.Equal(t, fmt.Sprintf("%x", sha256.Sum256([]byte(strings.TrimSpace(string(file.Data))))), checksum)
	}
}
