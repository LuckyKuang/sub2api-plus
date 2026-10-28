//go:build integration

package service

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"testing"
	"time"

	dbent "github.com/LuckyKuang/sub2api-plus/ent"
	"github.com/LuckyKuang/sub2api-plus/internal/testutil/postgresdsn"
	_ "github.com/lib/pq"
	"github.com/stretchr/testify/require"
)

type scopedTestSQL interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}
type scopedTestDB struct{ db *sql.DB }

func (s *scopedTestDB) sql(ctx context.Context) scopedTestSQL {
	if tx := dbent.TxFromContext(ctx); tx != nil {
		return tx.Client()
	}
	return s.db
}
func scopedAdminAccountTestDB(t *testing.T) (*scopedTestDB, *sql.DB) {
	t.Helper()
	dsn := os.Getenv("SCOPED_ADMIN_TEST_DSN")
	if dsn == "" {
		dsn = os.Getenv("SUB2API_TEST_POSTGRES_DSN")
	}
	if dsn == "" {
		t.Skip("需要独立 SCOPED_ADMIN_TEST_DSN")
	}
	base, err := sql.Open("postgres", dsn)
	require.NoError(t, err)
	t.Cleanup(func() { _ = base.Close() })
	name := fmt.Sprintf("scoped_accounts_%d", time.Now().UnixNano())
	_, err = base.Exec(`CREATE SCHEMA ` + name)
	require.NoError(t, err)
	t.Cleanup(func() { _, _ = base.Exec(`DROP SCHEMA ` + name + ` CASCADE`) })
	scopedDSN, err := postgresdsn.WithSearchPath(dsn, name)
	require.NoError(t, err)
	db, err := sql.Open("postgres", scopedDSN)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	_, err = db.Exec(`CREATE TABLE accounts(id BIGSERIAL PRIMARY KEY,name TEXT DEFAULT '',platform TEXT DEFAULT 'openai',type TEXT DEFAULT 'oauth',credentials JSONB DEFAULT '{}',status TEXT DEFAULT 'active',schedulable BOOLEAN DEFAULT true,error_message TEXT,temp_unschedulable_until TIMESTAMPTZ,temp_unschedulable_reason TEXT,deleted_at TIMESTAMPTZ,parent_account_id BIGINT,proxy_id BIGINT,expires_at TIMESTAMPTZ,updated_at TIMESTAMPTZ DEFAULT NOW());
 CREATE TABLE proxies(id BIGSERIAL PRIMARY KEY,protocol TEXT,host TEXT,port INT,username TEXT,password TEXT,status TEXT DEFAULT 'active',expires_at TIMESTAMPTZ,deleted_at TIMESTAMPTZ);`)
	require.NoError(t, err)
	return &scopedTestDB{db: db}, db
}
