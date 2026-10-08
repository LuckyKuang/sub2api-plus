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

// Approved audit: fix confidently local history once, without reclassifying
// providers or changing SLA flags, bodies, timestamps or unknown timing.
func TestClassifyLocalSecurityAuditDenialsMigration(t *testing.T) {
	dsn := os.Getenv("SUB2API_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("requires isolated PostgreSQL")
	}
	db, err := sql.Open("postgres", dsn)
	require.NoError(t, err)
	defer db.Close()
	migration, err := FS.ReadFile("279_classify_local_security_audit_denials.sql")
	require.NoError(t, err)
	ctx := context.Background()
	tx, err := db.BeginTx(ctx, nil)
	require.NoError(t, err)
	defer tx.Rollback()
	_, err = tx.ExecContext(ctx, `CREATE TEMP TABLE ops_error_logs (
 id int, status_code int DEFAULT 403, error_phase text DEFAULT 'internal', error_type text DEFAULT 'api_error',
 error_owner text DEFAULT 'platform', error_source text DEFAULT 'gateway', account_id bigint,
 upstream_status_code int, upstream_error_message text, upstream_error_detail text, upstream_errors jsonb,
 upstream_endpoint text, upstream_model text, error_body text, is_business_limited bool DEFAULT false,
 request_id text DEFAULT 'keep-request', created_at timestamptz DEFAULT '2026-10-07T00:00:00Z', time_to_first_token_ms int
 ) ON COMMIT DROP;`)
	require.NoError(t, err)
	fixtures := []struct {
		id                int
		body, extra, want string
	}{
		{1, `{"error":{"type":"content_policy_violation","message":"keep"}}`, "", "content_policy_violation"},
		{2, `{"error":{"type":"api_error","code":"session_blocked_by_content_policy"}}`, "", "session_blocked_by_content_policy"},
		{3, `{"error":{"type":"permission_error","code":"prompt_guard_blocked"}}`, "", "prompt_guard_blocked"},
		{4, `{"error":{"type":"content_policy_violation"}}`, "account_id=66", ""},
		{5, `{"error":{"type":"content_policy_violation"}}`, "upstream_status_code=403", ""},
		{6, `{"error":{"type":"content_policy_violation"}}`, `upstream_errors='[{"kind":"retry"}]'`, ""},
		{7, `{"error":{"type":"content_policy_violation"}}`, "error_phase='upstream',error_owner='provider'", ""},
		{8, `{"error":`, "", ""},
		{9, `{"error":{"code":"content_policy_violation","type":"invalid_request_error"}}`, "", ""},
		{10, `{"error":{"type":"api_error","code":"prompt_guard_unavailable"}}`, "status_code=503", ""},
		{11, `{"error":{"type":"api_error","code":"unrecognized"}}`, "", ""},
		{12, `{"error":{"type":"content_policy_violation"}}`, "upstream_endpoint='/v1/responses'", "content_policy_violation"},
		{13, `{"error":{"type":"content_policy_violation"}}`, "is_business_limited=true", "content_policy_violation"},
	}
	for _, f := range fixtures {
		_, err = tx.ExecContext(ctx, `INSERT INTO ops_error_logs(id,error_body) VALUES ($1,$2)`, f.id, f.body)
		require.NoError(t, err)
		if f.extra != "" {
			_, err = tx.ExecContext(ctx, "UPDATE ops_error_logs SET "+f.extra+" WHERE id=$1", f.id)
			require.NoError(t, err)
		}
	}
	before := map[int]string{}
	for _, f := range fixtures {
		var raw string
		require.NoError(t, tx.QueryRowContext(ctx, `SELECT row_to_json(t)::text FROM ops_error_logs t WHERE id=$1`, f.id).Scan(&raw))
		before[f.id] = raw
	}
	for repeat := 0; repeat < 2; repeat++ {
		_, err = tx.ExecContext(ctx, string(migration))
		require.NoError(t, err)
		for _, f := range fixtures {
			if f.want == "" {
				var raw string
				require.NoError(t, tx.QueryRowContext(ctx, `SELECT row_to_json(t)::text FROM ops_error_logs t WHERE id=$1`, f.id).Scan(&raw))
				require.JSONEq(t, before[f.id], raw)
				continue
			}
			var phase, kind, owner, source, body, requestID, timestamp string
			var limited bool
			var ttft sql.NullInt64
			require.NoError(t, tx.QueryRowContext(ctx, `SELECT error_phase,error_type,error_owner,error_source,error_body,request_id,to_char(created_at AT TIME ZONE 'UTC','YYYY-MM-DD HH24:MI:SS'),is_business_limited,time_to_first_token_ms FROM ops_error_logs WHERE id=$1`, f.id).Scan(&phase, &kind, &owner, &source, &body, &requestID, &timestamp, &limited, &ttft))
			require.Equal(t, "request", phase)
			require.Equal(t, f.want, kind)
			require.Equal(t, "client", owner)
			require.Equal(t, "client_request", source)
			require.Equal(t, f.body, body)
			require.Equal(t, "keep-request", requestID)
			require.Equal(t, "2026-10-07 00:00:00", timestamp)
			require.Equal(t, f.id == 13, limited)
			require.False(t, ttft.Valid)
		}
	}
}
