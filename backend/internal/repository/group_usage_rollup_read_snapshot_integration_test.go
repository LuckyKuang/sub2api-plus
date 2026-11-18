//go:build integration

package repository

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/lib/pq"
	"github.com/stretchr/testify/require"
)

// A group summary must describe one committed view of its watermark, published
// buckets and raw tail. In particular, a concurrent historical change and a
// current-day change cannot produce a total belonging to neither committed view.
func TestGroupUsageSummaryKeepsOneSnapshotAcrossConcurrentChanges(t *testing.T) {
	for _, tc := range []struct {
		name          string
		change        string
		wantTotal     float64
		wantYesterday float64
	}{
		{
			name: "late historical insert",
			change: `INSERT INTO usage_logs (id, user_id, group_id, actual_cost, created_at)
				VALUES (3, 1, 10, 4, TIMESTAMPTZ '2026-08-09 12:00:00+08')`,
			wantTotal: 29, wantYesterday: 5,
		},
		{
			name:      "historical delete",
			change:    `DELETE FROM usage_logs WHERE id = 1`,
			wantTotal: 24, wantYesterday: 0,
		},
		{
			name: "retention and bucket cleanup",
			change: `UPDATE usage_group_rollup_state
				SET expired_before = TIMESTAMPTZ '2026-08-10 00:00:00+08',
					retained_from = TIMESTAMPTZ '2026-08-10 00:00:00+08'
				WHERE id = 1;
				DELETE FROM usage_logs WHERE id = 1;
				DELETE FROM usage_group_daily_rollups WHERE bucket_date < DATE '2026-08-10'`,
			wantTotal: 24, wantYesterday: 0,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			useGroupUsageRepositoryTestTimezone(t, "Asia/Shanghai")
			schema := createGroupUsageRollupTriggerTestSchema(t, ctx, false)
			writer := openGroupUsageRollupSchemaDB(t, schema, "Asia/Shanghai")
			todayStart := time.Date(2026, 8, 9, 16, 0, 0, 0, time.UTC)
			execGroupUsageRollupSQL(t, ctx, writer, `
				INSERT INTO groups (id) VALUES (10);
				INSERT INTO users (id) VALUES (1);
				INSERT INTO usage_logs (id, user_id, group_id, actual_cost, created_at) VALUES
					(1, 1, 10, 1, TIMESTAMPTZ '2026-08-09 10:00:00+08'),
					(2, 1, 10, 8, TIMESTAMPTZ '2026-08-10 10:00:00+08');
				INSERT INTO usage_group_daily_rollups (bucket_date, group_id, actual_cost)
				VALUES (DATE '2026-08-09', 10, 1);
				UPDATE usage_group_rollup_state
				SET closed_before = DATE '2026-08-10',
					retained_from = TIMESTAMPTZ '2026-08-09 10:00:00+08',
					timezone_name = 'Asia/Shanghai'
				WHERE id = 1;
			`)

			// The driver hook runs after the repository has read its watermark,
			// immediately before it sends the aggregation query. Both writer
			// changes commit together, without timing assumptions or reader locks.
			reader := openGroupUsageSnapshotReader(t, schema, func(ctx context.Context) error {
				tx, err := writer.BeginTx(ctx, nil)
				if err != nil {
					return err
				}
				defer func() { _ = tx.Rollback() }()
				if _, err := tx.ExecContext(ctx, tc.change); err != nil {
					return err
				}
				if _, err := tx.ExecContext(ctx, `
					INSERT INTO usage_logs (id, user_id, group_id, actual_cost, created_at)
					VALUES (4, 1, 10, 16, TIMESTAMPTZ '2026-08-10 12:00:00+08')
				`); err != nil {
					return err
				}
				return tx.Commit()
			})

			before := groupUsageSummaryByGroup(t, ctx, reader, todayStart)
			require.Equal(t, [3]float64{9, 8, 1}, before[10],
				"the in-flight summary must retain the snapshot captured with its watermark")
			after := groupUsageSummaryByGroup(t, ctx, writer, todayStart)
			require.Equal(t, [3]float64{tc.wantTotal, 24, tc.wantYesterday}, after[10],
				"the next summary must observe the committed change and invalidated watermark")
		})
	}
}

// This test-only connector intercepts one aggregation query while preserving
// the real PostgreSQL transaction and its isolation semantics.
type groupUsageSnapshotConnector struct {
	driver.Connector
	beforeAggregate func(context.Context) error
	once            sync.Once
}

func (c *groupUsageSnapshotConnector) Connect(ctx context.Context) (driver.Conn, error) {
	conn, err := c.Connector.Connect(ctx)
	if err != nil {
		return nil, err
	}
	return &groupUsageSnapshotConn{Conn: conn, connector: c}, nil
}

type groupUsageSnapshotConn struct {
	driver.Conn
	connector *groupUsageSnapshotConnector
}

func (c *groupUsageSnapshotConn) BeginTx(ctx context.Context, opts driver.TxOptions) (driver.Tx, error) {
	return c.Conn.(driver.ConnBeginTx).BeginTx(ctx, opts)
}

func (c *groupUsageSnapshotConn) QueryContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	if strings.Contains(query, "WITH historical AS") {
		var err error
		c.connector.once.Do(func() { err = c.connector.beforeAggregate(ctx) })
		if err != nil {
			return nil, err
		}
	}
	return c.Conn.(driver.QueryerContext).QueryContext(ctx, query, args)
}

func openGroupUsageSnapshotReader(t *testing.T, schema string, beforeAggregate func(context.Context) error) *sql.DB {
	t.Helper()
	dsn, err := url.Parse(integrationDSN)
	require.NoError(t, err)
	query := dsn.Query()
	query.Set("search_path", schema)
	query.Set("TimeZone", "Asia/Shanghai")
	dsn.RawQuery = query.Encode()
	connector, err := pq.NewConnector(dsn.String())
	require.NoError(t, err)
	db := sql.OpenDB(&groupUsageSnapshotConnector{Connector: connector, beforeAggregate: beforeAggregate})
	t.Cleanup(func() { _ = db.Close() })
	return db
}
