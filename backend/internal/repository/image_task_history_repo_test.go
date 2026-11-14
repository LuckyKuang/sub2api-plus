//go:build unit || !integration

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
			"task_id", "user_id", "api_key_id", "request_type", "model", "platform", "prompt_preview",
			"status", "http_status", "image_url", "result", "error",
			"storage_keys", "requested_images", "actual_images",
			"created_at", "completed_at", "expires_at",
		}).AddRow(
			"imgtask_failed", owner.UserID, owner.APIKeyID, "generation", "gpt-image-2", "openai", "prompt",
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

// The console reads one user's history without a key scope, so the query must not
// constrain api_key_id; the gateway path keeps it.
func TestImageTaskHistoryRepositoryGetAllowsUserScope(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	repo := &imageTaskHistoryRepository{db: db}
	owner := service.ImageTaskOwner{UserID: 7}
	now := time.Now().UTC().Truncate(time.Microsecond)
	query := asyncImageTaskSelectSQL + `
WHERE task_id = $1 AND user_id = $2`
	mock.ExpectQuery(regexp.QuoteMeta(query)).
		WithArgs("imgtask_any_key", owner.UserID).
		WillReturnRows(sqlmock.NewRows([]string{
			"task_id", "user_id", "api_key_id", "request_type", "model", "platform", "prompt_preview",
			"status", "http_status", "image_url", "result", "error",
			"storage_keys", "requested_images", "actual_images",
			"created_at", "completed_at", "expires_at",
		}).AddRow(
			"imgtask_any_key", owner.UserID, 9, "generation", "gpt-image-2", "openai", "prompt",
			service.ImageTaskStatusCompleted, 200, "https://example.test/i.png", nil, nil,
			`["images/i.png"]`, 1, 1,
			now, now, now.Add(time.Hour),
		))

	task, err := repo.Get(context.Background(), owner, "imgtask_any_key")
	require.NoError(t, err)
	require.Equal(t, "openai", task.Platform)
	require.Equal(t, []string{"images/i.png"}, task.StorageKeys)
	require.NoError(t, mock.ExpectationsWereMet())
}

// Platform is the console filter; it must narrow the query instead of filtering in
// memory so pagination stays correct.
func TestImageTaskHistoryRepositoryListFiltersByPlatform(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	repo := &imageTaskHistoryRepository{db: db}
	owner := service.ImageTaskOwner{UserID: 7}
	query := asyncImageTaskSelectSQL + " WHERE user_id = $1 AND platform = $2 ORDER BY created_at DESC, id DESC LIMIT $3 OFFSET $4"
	mock.ExpectQuery(regexp.QuoteMeta(query)).
		WithArgs(owner.UserID, "grok", 21, 0).
		WillReturnRows(sqlmock.NewRows([]string{
			"task_id", "user_id", "api_key_id", "request_type", "model", "platform", "prompt_preview",
			"status", "http_status", "image_url", "result", "error",
			"storage_keys", "requested_images", "actual_images",
			"created_at", "completed_at", "expires_at",
		}))

	tasks, hasMore, err := repo.List(context.Background(), owner, service.ImageTaskHistoryFilter{Limit: 20, Platform: "grok"})
	require.NoError(t, err)
	require.Empty(t, tasks)
	require.False(t, hasMore)
	require.NoError(t, mock.ExpectationsWereMet())
}
