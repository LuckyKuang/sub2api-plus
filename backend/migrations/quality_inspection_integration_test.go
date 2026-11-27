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

// MODEL_QUALITY_INSPECTION requires one plan per group/model, one run per slot,
// cascading plan history deletion, and no changes to account schedulability.
func TestQualityInspectionMigration(t *testing.T) {
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
	_, err = tx.Exec(`CREATE SCHEMA quality_inspection_migration_test; SET LOCAL search_path TO quality_inspection_migration_test; CREATE TABLE accounts (id bigint PRIMARY KEY, schedulable boolean NOT NULL); INSERT INTO accounts VALUES (1, true);`)
	require.NoError(t, err)
	raw, err := FS.ReadFile("318_quality_inspection.sql")
	require.NoError(t, err)
	for i := 0; i < 2; i++ {
		_, err = tx.Exec(string(raw))
		require.NoError(t, err)
	}
	var planID, runID int64
	require.NoError(t, tx.QueryRow(`INSERT INTO quality_inspection_plans (group_id,name,model) VALUES (1,'Check','model-a') RETURNING id`).Scan(&planID))
	require.NoError(t, tx.QueryRow(`INSERT INTO quality_inspection_runs (plan_id,trigger,slot) VALUES ($1,'manual',1) RETURNING id`, planID).Scan(&runID))
	_, err = tx.Exec(`INSERT INTO quality_inspection_results (run_id,plan_id,account_id,question_id,kind,status) VALUES ($1,$2,1,'numeric','number','pass')`, runID, planID)
	require.NoError(t, err)
	// ON CONFLICT leaves the transaction usable while proving independent keys.
	var count int
	require.NoError(t, tx.QueryRow(`WITH inserted AS (INSERT INTO quality_inspection_plans (group_id,name,model) VALUES (1,'Duplicate','model-a') ON CONFLICT (group_id,model) DO NOTHING RETURNING id) SELECT count(*) FROM inserted`).Scan(&count))
	require.Zero(t, count)
	require.NoError(t, tx.QueryRow(`WITH inserted AS (INSERT INTO quality_inspection_runs (plan_id,trigger,slot) VALUES ($1,'manual',1) ON CONFLICT (plan_id,trigger,slot) DO NOTHING RETURNING id) SELECT count(*) FROM inserted`, planID).Scan(&count))
	require.Zero(t, count)
	_, err = tx.Exec(`DELETE FROM quality_inspection_plans WHERE id=$1`, planID)
	require.NoError(t, err)
	require.NoError(t, tx.QueryRow(`SELECT count(*) FROM quality_inspection_results`).Scan(&count))
	require.Zero(t, count)
	require.NoError(t, tx.QueryRow(`SELECT count(*) FROM quality_inspection_runs`).Scan(&count))
	require.Zero(t, count)
	var schedulable bool
	require.NoError(t, tx.QueryRow(`SELECT schedulable FROM accounts WHERE id=1`).Scan(&schedulable))
	require.True(t, schedulable)
}
