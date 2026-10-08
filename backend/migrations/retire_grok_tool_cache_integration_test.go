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

// Business requirement: retire only Grok's obsolete cache key, preserving
// credentials, unrelated extras, foreign accounts and malformed historical data.
func TestRetireGrokToolCacheMigrationPreservesAccountData(t *testing.T) {
	dsn := os.Getenv("SUB2API_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("requires isolated PostgreSQL via SUB2API_TEST_POSTGRES_DSN")
	}
	db, err := sql.Open("postgres", dsn)
	require.NoError(t, err)
	defer db.Close()
	tx, err := db.BeginTx(context.Background(), nil)
	require.NoError(t, err)
	defer tx.Rollback()
	_, err = tx.Exec(`CREATE TEMP TABLE accounts(id int, platform text, credentials jsonb, extra jsonb) ON COMMIT DROP`)
	require.NoError(t, err)
	cases := []struct{ platform, before, after string }{
		{"grok", `{"grok_client_tool_cache_enabled":true,"custom":"keep"}`, `{"custom":"keep"}`},
		{"grok", `{"grok_client_tool_cache_enabled":false}`, `{}`},
		{"grok", `{"grok_client_tool_cache_enabled":"true","custom":17}`, `{"custom":17}`},
		{"grok", `{"custom":"keep"}`, `{"custom":"keep"}`},
		{"openai", `{"grok_client_tool_cache_enabled":true}`, `{"grok_client_tool_cache_enabled":true}`},
		{"grok", `["grok_client_tool_cache_enabled"]`, `["grok_client_tool_cache_enabled"]`},
		{"grok", `"grok_client_tool_cache_enabled"`, `"grok_client_tool_cache_enabled"`},
		{"grok", `null`, `null`},
	}
	for id, tc := range cases {
		_, err = tx.Exec(`INSERT INTO accounts VALUES($1,$2,'{"api_key":"unchanged","outbound_identity":{"preset":"grok"}}',$3::jsonb)`, id, tc.platform, tc.before)
		require.NoError(t, err)
	}
	migration, err := FS.ReadFile("280_retire_grok_tool_cache_control.sql")
	require.NoError(t, err)
	for range 2 { // forward cleanup must also be idempotent
		_, err = tx.Exec(string(migration))
		require.NoError(t, err)
	}
	for id, tc := range cases {
		var extra, credentials string
		require.NoError(t, tx.QueryRow(`SELECT extra::text,credentials::text FROM accounts WHERE id=$1`, id).Scan(&extra, &credentials))
		require.JSONEq(t, tc.after, extra)
		require.JSONEq(t, `{"api_key":"unchanged","outbound_identity":{"preset":"grok"}}`, credentials)
	}
}
