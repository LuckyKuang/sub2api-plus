//go:build unit

package repository

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/LuckyKuang/sub2api-plus/internal/service"
	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

// R6: a database outage also makes the database retry table unavailable. The
// compensation payload must survive an execution-key TTL and a worker restart.
func TestImageTaskHistoryRetryRepositorySurvivesDatabaseOutageAndCacheExpiry(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })
	repo := NewImageTaskHistoryRetryRepository(db, rdb)
	record := &service.ImageTaskRecord{ID: "imgtask_outage", UserID: 7, APIKeyID: 9, Status: service.ImageTaskStatusCompleted, Result: json.RawMessage(`{"data":[{"url":"https://old.test/expired"}]}`), StorageKeys: []string{"original-prefix/imgtask_outage-0.png"}}
	mock.ExpectExec("INSERT INTO async_image_task_history_retry").WillReturnError(errors.New("postgres unavailable"))
	require.NoError(t, repo.Enqueue(context.Background(), record))
	require.Zero(t, mr.TTL(imageHistoryRetryRedisKey), "compensation must have no execution-cache TTL")
	require.NoError(t, rdb.Set(context.Background(), imageTaskKey(record.ID), "execution", time.Hour).Err())
	mr.FastForward(48 * time.Hour)
	require.False(t, mr.Exists(imageTaskKey(record.ID)))
	restarted := NewImageTaskHistoryRetryRepository(db, rdb)
	mock.ExpectQuery("SELECT payload::text, attempts").WillReturnRows(sqlmock.NewRows([]string{"payload", "attempts"}))
	pending, err := restarted.Due(context.Background(), 20)
	require.NoError(t, err)
	require.Len(t, pending, 1)
	require.Equal(t, record, pending[0].Record)
	mock.ExpectExec("DELETE FROM async_image_task_history_retry").WithArgs(record.ID).WillReturnResult(sqlmock.NewResult(0, 0))
	require.NoError(t, restarted.Delete(context.Background(), record.ID))
	require.False(t, mr.Exists(imageHistoryRetryRedisKey))
	require.NoError(t, mock.ExpectationsWereMet())
}

// A completion replay can update only an existing accepted row. In particular,
// replay after failed-task deletion must not recreate the deleted history.
func TestImageTaskHistoryRepositoryTerminalSaveOnlyUpdatesExistingHistory(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	repo := NewImageTaskHistoryRepository(db)
	mock.ExpectExec("UPDATE async_image_tasks SET").WillReturnResult(sqlmock.NewResult(0, 0))
	require.NoError(t, repo.Save(context.Background(), &service.ImageTaskRecord{ID: "imgtask_deleted", UserID: 7, APIKeyID: 9, Status: service.ImageTaskStatusFailed}))
	require.NoError(t, mock.ExpectationsWereMet())
}
