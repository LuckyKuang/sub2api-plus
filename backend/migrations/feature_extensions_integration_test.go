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

// Approved upgrade contract: historical codes remain opt-out, payment orders
// retain their source/currency resolution, and V3 history/incident IDs survive
// while equally numbered request and probe scopes can coexist.
func TestFeatureExtensionsUpgradePreservesHistoricalData(t *testing.T) {
	dsn := os.Getenv("SUB2API_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("requires isolated PostgreSQL via SUB2API_TEST_POSTGRES_DSN")
	}
	ctx := context.Background()
	db, err := sql.Open("postgres", dsn)
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	tx, err := db.BeginTx(ctx, nil)
	require.NoError(t, err)
	defer func() { _ = tx.Rollback() }()
	// Temp tables isolate these pre-upgrade fixtures from application tables.
	_, err = tx.ExecContext(ctx, `
CREATE TEMP TABLE redeem_codes (id bigint PRIMARY KEY, code text, value numeric(20,2)) ON COMMIT DROP;
CREATE TEMP TABLE payment_orders (id bigint PRIMARY KEY, pay_amount numeric(20,2), paid_at timestamptz, provider_snapshot jsonb) ON COMMIT DROP;
CREATE TEMP TABLE channel_monitor_v3_facts (
 bucket_start timestamptz NOT NULL, platform text NOT NULL, group_id bigint NOT NULL, model text NOT NULL,
 success_requests bigint NOT NULL, failed_requests bigint NOT NULL, last_request_at timestamptz NOT NULL, ttft_counts bigint[] NOT NULL,
 PRIMARY KEY (bucket_start,platform,group_id,model)) ON COMMIT DROP;
CREATE TEMP TABLE channel_monitor_v3_states (
 platform text NOT NULL, group_id bigint NOT NULL, model text NOT NULL, data jsonb NOT NULL,
 PRIMARY KEY (platform,group_id,model)) ON COMMIT DROP;
CREATE TEMP TABLE channel_monitor_v3_incidents (
 id text PRIMARY KEY, platform text NOT NULL, group_id bigint NOT NULL, model text NOT NULL,
 started_at timestamptz NOT NULL, resolved_at timestamptz, data jsonb NOT NULL) ON COMMIT DROP;
CREATE UNIQUE INDEX idx_channel_monitor_v3_active_incident ON channel_monitor_v3_incidents (platform,group_id,model) WHERE resolved_at IS NULL;
INSERT INTO redeem_codes VALUES (1,'historical-code',100);
INSERT INTO payment_orders VALUES (1,50,'2026-01-01 00:00:00+00','{"currency":"USD"}');
INSERT INTO channel_monitor_v3_facts VALUES ('2026-01-01 00:00:00+00','openai',7,'model',4,1,'2026-01-01 00:00:10+00',array_fill(0::bigint,ARRAY[16]));
INSERT INTO channel_monitor_v3_states VALUES ('openai',7,'model','{"streak":2}');
INSERT INTO channel_monitor_v3_incidents VALUES ('old-event','openai',7,'model','2026-01-01 00:00:00+00',NULL,'{"severity":"partial"}');`)
	require.NoError(t, err)
	for _, name := range []string{"282_redeem_order_snapshots.sql", "283_channel_monitor_v3_probe_sources.sql"} {
		migration, readErr := FS.ReadFile(name)
		require.NoError(t, readErr)
		_, err = tx.ExecContext(ctx, string(migration))
		require.NoError(t, err)
	}
	var generate bool
	var currency, source, snapshot string
	var value, amount float64
	require.NoError(t, tx.QueryRowContext(ctx, `SELECT generate_order,order_currency,value,order_amount FROM redeem_codes WHERE id=1`).Scan(&generate, &currency, &value, &amount))
	require.False(t, generate)
	require.Equal(t, "CNY", currency)
	require.Equal(t, 100.0, value)
	require.Zero(t, amount)
	require.NoError(t, tx.QueryRowContext(ctx, `SELECT order_source,currency,pay_amount,provider_snapshot::text FROM payment_orders WHERE id=1`).Scan(&source, &currency, &amount, &snapshot))
	require.Equal(t, "payment", source)
	require.Empty(t, currency, "historical provider snapshot remains authoritative")
	require.Equal(t, 50.0, amount)
	require.JSONEq(t, `{"currency":"USD"}`, snapshot)
	var scope, success, failures int64
	require.NoError(t, tx.QueryRowContext(ctx, `SELECT source_kind,scope_id,success_requests,failed_requests FROM channel_monitor_v3_facts`).Scan(&source, &scope, &success, &failures))
	require.Equal(t, "request", source)
	require.Equal(t, int64(7), scope)
	require.Equal(t, int64(4), success)
	require.Equal(t, int64(1), failures)
	require.NoError(t, tx.QueryRowContext(ctx, `SELECT source_kind,scope_id,data::text FROM channel_monitor_v3_states`).Scan(&source, &scope, &snapshot))
	require.Equal(t, "request", source)
	require.Equal(t, int64(7), scope)
	require.JSONEq(t, `{"streak":2}`, snapshot)
	require.NoError(t, tx.QueryRowContext(ctx, `SELECT source_kind,scope_id,data::text FROM channel_monitor_v3_incidents WHERE id='old-event'`).Scan(&source, &scope, &snapshot))
	require.Equal(t, "request", source)
	require.Equal(t, int64(7), scope)
	require.JSONEq(t, `{"severity":"partial"}`, snapshot)
	_, err = tx.ExecContext(ctx, `
INSERT INTO channel_monitor_v3_facts (bucket_start,platform,source_kind,scope_id,group_id,model,success_requests,failed_requests,last_request_at,ttft_counts,probe_status)
 VALUES ('2026-01-01 00:00:00+00','openai','probe',7,NULL,'model',0,1,'2026-01-01 00:00:10+00',array_fill(0::bigint,ARRAY[16]),'failed');
INSERT INTO channel_monitor_v3_states (platform,source_kind,scope_id,group_id,model,data) VALUES ('openai','probe',7,NULL,'model','{}');
INSERT INTO channel_monitor_v3_incidents (id,platform,source_kind,scope_id,group_id,model,started_at,data)
 VALUES ('probe-event','openai','probe',7,NULL,'model','2026-01-01 00:00:00+00','{}');
INSERT INTO payment_orders (id,redeem_code_id) VALUES (2,42);`)
	require.NoError(t, err, "request group 7 and probe monitor 7 are distinct scopes")
	result, err := tx.ExecContext(ctx, `INSERT INTO payment_orders (id,redeem_code_id) VALUES (3,42) ON CONFLICT DO NOTHING`)
	require.NoError(t, err)
	rows, err := result.RowsAffected()
	require.NoError(t, err)
	require.Zero(t, rows, "one code can produce at most one order")
	result, err = tx.ExecContext(ctx, `INSERT INTO channel_monitor_v3_incidents (id,platform,source_kind,scope_id,model,started_at,data)
 VALUES ('duplicate-probe','openai','probe',7,'model','2026-01-01 00:00:00+00','{}') ON CONFLICT DO NOTHING`)
	require.NoError(t, err)
	rows, err = result.RowsAffected()
	require.NoError(t, err)
	require.Zero(t, rows, "one scope cannot have two active incidents")
}
