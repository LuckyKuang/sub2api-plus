//go:build unit

package repository

import (
	"context"
	"database/sql"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/LuckyKuang/sub2api-plus/internal/service"
	"github.com/stretchr/testify/require"
)

func TestOpsInsertErrorLogArgsPreservesExplicitZeroUpstreamStatus(t *testing.T) {
	zero := 0
	args := opsInsertErrorLogArgs(&service.OpsInsertErrorLogInput{UpstreamStatusCode: &zero})

	require.Len(t, args, 39)
	encoded, ok := args[28].(sql.NullInt64)
	require.True(t, ok)
	require.True(t, encoded.Valid)
	require.Zero(t, encoded.Int64)
}

func TestOpsNullableIntPointerDistinguishesNilZeroAndStatus(t *testing.T) {
	missing := opsNullableIntPointer(nil).(sql.NullInt64)
	require.False(t, missing.Valid)

	zeroValue := 0
	zero := opsNullableIntPointer(&zeroValue).(sql.NullInt64)
	require.True(t, zero.Valid)
	require.Zero(t, zero.Int64)

	statusValue := 503
	status := opsNullableIntPointer(&statusValue).(sql.NullInt64)
	require.True(t, status.Valid)
	require.EqualValues(t, 503, status.Int64)
}

func TestInsertErrorLogTreatsAsyncTaskUniqueConflictAsIdempotent(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	repo := NewOpsRepository(db).(*opsRepository)
	mock.ExpectQuery("INSERT INTO ops_error_logs").
		WillReturnRows(sqlmock.NewRows([]string{"id"}))

	id, err := repo.InsertErrorLog(context.Background(), &service.OpsInsertErrorLogInput{
		AsyncTaskID: "imgtask_duplicate",
		ErrorPhase:  "upstream",
		ErrorType:   "upstream_error",
	})
	require.NoError(t, err)
	require.Zero(t, id)
	require.NoError(t, mock.ExpectationsWereMet())
	require.Contains(t, insertOpsErrorLogSQL, "ON CONFLICT (async_task_id) WHERE async_task_id IS NOT NULL DO NOTHING")
}
