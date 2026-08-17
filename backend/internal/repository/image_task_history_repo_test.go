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
			"created_at", "completed_at", "expires_at",
		}).AddRow(
			"imgtask_failed", owner.UserID, owner.APIKeyID, "generation", "gpt-image-2", "prompt",
			service.ImageTaskStatusFailed, 502, nil, nil, `{"type":"upstream_error"}`,
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
