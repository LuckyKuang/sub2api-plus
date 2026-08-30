package repository

import (
	"context"
	"database/sql"
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/LuckyKuang/sub2api-plus/internal/service"
	"github.com/stretchr/testify/require"
)

func TestImageTaskHistoryRepositoryGetScopesByOwner(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	repo := &imageTaskHistoryRepository{db: db}
	owner := service.ImageTaskOwner{UserID: 7, APIKeyID: 9}
	now := time.Now().UTC().Truncate(time.Microsecond)
	query := asyncImageTaskSelectSQL + `
WHERE task_id = $1 AND user_id = $2 AND api_key_id = $3`
	mock.ExpectQuery(regexp.QuoteMeta(query)).
		WithArgs("imgtask_failed", owner.UserID, owner.APIKeyID).
		WillReturnRows(sqlmock.NewRows([]string{
			"task_id", "user_id", "api_key_id", "request_type", "model", "prompt_preview",
			"status", "http_status", "image_url", "result", "error",
			"storage_keys", "requested_images", "actual_images",
			"created_at", "completed_at", "expires_at",
		}).AddRow(
			"imgtask_failed", owner.UserID, owner.APIKeyID, "generation", "gpt-image-2", "prompt",
			service.ImageTaskStatusFailed, 502, nil, nil, `{"type":"upstream_error"}`,
			`[]`, 2, 0,
			now, now, now.Add(time.Hour),
		))

	task, err := repo.Get(context.Background(), owner, "imgtask_failed")
	require.NoError(t, err)
	require.Equal(t, "imgtask_failed", task.ID)
	require.Equal(t, service.ImageTaskStatusFailed, task.Status)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestImageTaskHistoryRepositoryGetHidesMissingOwner(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	repo := &imageTaskHistoryRepository{db: db}
	owner := service.ImageTaskOwner{UserID: 7, APIKeyID: 10}
	query := asyncImageTaskSelectSQL + `
WHERE task_id = $1 AND user_id = $2 AND api_key_id = $3`
	mock.ExpectQuery(regexp.QuoteMeta(query)).
		WithArgs("imgtask_other", owner.UserID, owner.APIKeyID).
		WillReturnError(sql.ErrNoRows)

	_, err = repo.Get(context.Background(), owner, "imgtask_other")
	require.ErrorIs(t, err, service.ErrImageTaskNotFound)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestImageTaskHistoryRepositoryListByUserDoesNotRequireAPIKey(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	repo := &imageTaskHistoryRepository{db: db}
	now := time.Now().UTC().Truncate(time.Microsecond)
	query := asyncImageTaskSelectSQL + ` WHERE user_id = $1 AND status = $2 ORDER BY created_at DESC, id DESC LIMIT $3 OFFSET $4`
	mock.ExpectQuery(regexp.QuoteMeta(query)).
		WithArgs(int64(7), service.ImageTaskStatusCompleted, 21, 0).
		WillReturnRows(sqlmock.NewRows([]string{
			"task_id", "user_id", "api_key_id", "request_type", "model", "prompt_preview",
			"status", "http_status", "image_url", "result", "error",
			"storage_keys", "requested_images", "actual_images",
			"created_at", "completed_at", "expires_at",
		}).AddRow(
			"imgtask_user", 7, 99, "generation", "gpt-image-2", "prompt",
			service.ImageTaskStatusCompleted, 200, "https://example.test/image.png", `{"data":[{"url":"https://example.test/image.png"}]}`, nil,
			`["images/task.png"]`, 2, 1,
			now, now, now.Add(time.Hour),
		))

	tasks, hasMore, err := repo.ListByUser(context.Background(), 7, service.ImageTaskHistoryFilter{Status: service.ImageTaskStatusCompleted})
	require.NoError(t, err)
	require.False(t, hasMore)
	require.Len(t, tasks, 1)
	require.Equal(t, int64(99), tasks[0].APIKeyID)
	require.Equal(t, []string{"images/task.png"}, tasks[0].StorageKeys)
	require.Equal(t, 2, tasks[0].RequestedImages)
	require.Equal(t, 1, tasks[0].ActualImages)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestImageTaskHistoryRepositoryGetByUserScopesTaskToTarget(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	repo := &imageTaskHistoryRepository{db: db}
	query := asyncImageTaskSelectSQL + `
WHERE task_id = $1 AND user_id = $2`
	mock.ExpectQuery(regexp.QuoteMeta(query)).
		WithArgs("imgtask_other", int64(7)).
		WillReturnError(sql.ErrNoRows)

	_, err = repo.GetByUser(context.Background(), 7, "imgtask_other")
	require.ErrorIs(t, err, service.ErrImageTaskNotFound)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestImageTaskHistoryRepositoryDeleteFailedUsesOwnerAndStatus(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	repo := &imageTaskHistoryRepository{db: db}
	owner := service.ImageTaskOwner{UserID: 7, APIKeyID: 9}
	query := `
DELETE FROM async_image_tasks
WHERE task_id = $1 AND user_id = $2 AND api_key_id = $3 AND status = $4`
	mock.ExpectExec(regexp.QuoteMeta(query)).
		WithArgs("imgtask_failed", owner.UserID, owner.APIKeyID, service.ImageTaskStatusFailed).
		WillReturnResult(sqlmock.NewResult(0, 1))

	deleted, err := repo.DeleteFailed(context.Background(), owner, "imgtask_failed")
	require.NoError(t, err)
	require.True(t, deleted)
	require.NoError(t, mock.ExpectationsWereMet())
}
