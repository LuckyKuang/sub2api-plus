package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/service"
)

type imageTaskHistoryRepository struct {
	db *sql.DB
}

func NewImageTaskHistoryRepository(db *sql.DB) service.ImageTaskHistoryRepository {
	return &imageTaskHistoryRepository{db: db}
}

func (r *imageTaskHistoryRepository) Save(ctx context.Context, task *service.ImageTaskRecord) error {
	if r == nil || r.db == nil {
		return fmt.Errorf("async image task history database is unavailable")
	}
	if task == nil || task.ID == "" || task.UserID <= 0 || task.APIKeyID <= 0 {
		return fmt.Errorf("invalid async image task history record")
	}
	storageKeyValues := task.StorageKeys
	if storageKeyValues == nil {
		storageKeyValues = []string{}
	}
	storageKeys, err := json.Marshal(storageKeyValues)
	if err != nil {
		return fmt.Errorf("marshal async image task storage keys: %w", err)
	}

	_, err = r.db.ExecContext(ctx, `
INSERT INTO async_image_tasks (
    task_id, user_id, api_key_id, request_type, model, prompt_preview,
    status, http_status, image_url, result, error, storage_keys, requested_images, actual_images,
    created_at, completed_at, expires_at, updated_at
) VALUES (
    $1, $2, $3, $4, $5, NULLIF($6, ''),
    $7, NULLIF($8, 0), NULLIF($9, ''), $10::jsonb, $11::jsonb, $12::jsonb, $13, $14,
    $15, $16, $17, NOW()
)
ON CONFLICT (task_id) DO UPDATE SET
    request_type = EXCLUDED.request_type,
    model = EXCLUDED.model,
    prompt_preview = EXCLUDED.prompt_preview,
    status = EXCLUDED.status,
    http_status = EXCLUDED.http_status,
    image_url = EXCLUDED.image_url,
    result = EXCLUDED.result,
    error = EXCLUDED.error,
    storage_keys = EXCLUDED.storage_keys,
    requested_images = EXCLUDED.requested_images,
    actual_images = EXCLUDED.actual_images,
    completed_at = EXCLUDED.completed_at,
    expires_at = EXCLUDED.expires_at,
    updated_at = NOW()`,
		task.ID,
		task.UserID,
		task.APIKeyID,
		task.RequestType,
		task.Model,
		task.PromptPreview,
		task.Status,
		task.HTTPStatus,
		imageTaskFirstURL(task.Result),
		serviceImageTaskJSONText(task.Result),
		serviceImageTaskJSONText(task.Error),
		string(storageKeys),
		task.RequestedImages,
		task.ActualImages,
		time.Unix(task.CreatedAt, 0).UTC(),
		imageTaskHistoryCompletedAt(task.CompletedAt),
		time.Unix(task.ExpiresAt, 0).UTC(),
	)
	return err
}

func (r *imageTaskHistoryRepository) List(ctx context.Context, owner service.ImageTaskOwner, filter service.ImageTaskHistoryFilter) ([]*service.ImageTaskRecord, bool, error) {
	if r == nil || r.db == nil {
		return nil, false, fmt.Errorf("async image task history database is unavailable")
	}
	filter = normalizeRepositoryImageTaskFilter(filter)
	query := asyncImageTaskSelectSQL + ` WHERE user_id = $1 AND api_key_id = $2`
	args := []any{owner.UserID, owner.APIKeyID}
	if filter.Status != "" {
		query += " AND status = $" + strconv.Itoa(len(args)+1)
		args = append(args, filter.Status)
	}
	query += " ORDER BY created_at DESC, id DESC LIMIT $" + strconv.Itoa(len(args)+1) + " OFFSET $" + strconv.Itoa(len(args)+2)
	args = append(args, filter.Limit+1, filter.Offset)

	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, false, err
	}
	defer func() { _ = rows.Close() }()

	tasks := make([]*service.ImageTaskRecord, 0, filter.Limit)
	for rows.Next() {
		task, scanErr := scanImageTaskHistory(rows)
		if scanErr != nil {
			return nil, false, scanErr
		}
		tasks = append(tasks, task)
	}
	if err := rows.Err(); err != nil {
		return nil, false, err
	}
	hasMore := len(tasks) > filter.Limit
	if hasMore {
		tasks = tasks[:filter.Limit]
	}
	return tasks, hasMore, nil
}

func (r *imageTaskHistoryRepository) Get(ctx context.Context, owner service.ImageTaskOwner, id string) (*service.ImageTaskRecord, error) {
	if r == nil || r.db == nil {
		return nil, fmt.Errorf("async image task history database is unavailable")
	}
	task, err := scanImageTaskHistory(r.db.QueryRowContext(ctx, asyncImageTaskSelectSQL+`
WHERE task_id = $1 AND user_id = $2 AND api_key_id = $3`, id, owner.UserID, owner.APIKeyID))
	if err == sql.ErrNoRows {
		return nil, service.ErrImageTaskNotFound
	}
	return task, err
}

// ListByUser is the administrator support-view read path. It is intentionally
// scoped only by user ID so support staff never need the target user's API-key
// value. The ordinary owner path above remains scoped by both user and key.
func (r *imageTaskHistoryRepository) ListByUser(ctx context.Context, userID int64, filter service.ImageTaskHistoryFilter) ([]*service.ImageTaskRecord, bool, error) {
	if r == nil || r.db == nil {
		return nil, false, fmt.Errorf("async image task history database is unavailable")
	}
	filter = normalizeRepositoryImageTaskFilter(filter)
	query := asyncImageTaskSelectSQL + ` WHERE user_id = $1`
	args := []any{userID}
	if filter.Status != "" {
		query += " AND status = $" + strconv.Itoa(len(args)+1)
		args = append(args, filter.Status)
	}
	query += " ORDER BY created_at DESC, id DESC LIMIT $" + strconv.Itoa(len(args)+1) + " OFFSET $" + strconv.Itoa(len(args)+2)
	args = append(args, filter.Limit+1, filter.Offset)

	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, false, err
	}
	defer func() { _ = rows.Close() }()

	tasks := make([]*service.ImageTaskRecord, 0, filter.Limit)
	for rows.Next() {
		task, scanErr := scanImageTaskHistory(rows)
		if scanErr != nil {
			return nil, false, scanErr
		}
		tasks = append(tasks, task)
	}
	if err := rows.Err(); err != nil {
		return nil, false, err
	}
	hasMore := len(tasks) > filter.Limit
	if hasMore {
		tasks = tasks[:filter.Limit]
	}
	return tasks, hasMore, nil
}

// GetByUser returns a durable task only when it belongs to the selected target
// account. It does not read or repair Redis execution state.
func (r *imageTaskHistoryRepository) GetByUser(ctx context.Context, userID int64, id string) (*service.ImageTaskRecord, error) {
	if r == nil || r.db == nil {
		return nil, fmt.Errorf("async image task history database is unavailable")
	}
	task, err := scanImageTaskHistory(r.db.QueryRowContext(ctx, asyncImageTaskSelectSQL+`
WHERE task_id = $1 AND user_id = $2`, id, userID))
	if err == sql.ErrNoRows {
		return nil, service.ErrImageTaskNotFound
	}
	return task, err
}

func (r *imageTaskHistoryRepository) DeleteFailed(ctx context.Context, owner service.ImageTaskOwner, id string) (bool, error) {
	if r == nil || r.db == nil {
		return false, fmt.Errorf("async image task history database is unavailable")
	}
	result, err := r.db.ExecContext(ctx, `
DELETE FROM async_image_tasks
WHERE task_id = $1 AND user_id = $2 AND api_key_id = $3 AND status = $4`,
		id, owner.UserID, owner.APIKeyID, service.ImageTaskStatusFailed)
	if err != nil {
		return false, err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return false, err
	}
	return affected > 0, nil
}

const asyncImageTaskColumns = `
task_id, user_id, api_key_id, request_type, model, prompt_preview,
status, http_status, image_url, result::text, error::text,
storage_keys::text, requested_images, actual_images,
created_at, completed_at, expires_at`

const asyncImageTaskSelectSQL = `SELECT ` + asyncImageTaskColumns + ` FROM async_image_tasks`

func scanImageTaskHistory(row interface{ Scan(...any) error }) (*service.ImageTaskRecord, error) {
	var task service.ImageTaskRecord
	var promptPreview, imageURL, result, taskError, storageKeys sql.NullString
	var httpStatus sql.NullInt64
	var completedAt sql.NullTime
	var createdAt, expiresAt time.Time
	if err := row.Scan(
		&task.ID, &task.UserID, &task.APIKeyID, &task.RequestType, &task.Model, &promptPreview,
		&task.Status, &httpStatus, &imageURL, &result, &taskError,
		&storageKeys, &task.RequestedImages, &task.ActualImages,
		&createdAt, &completedAt, &expiresAt,
	); err != nil {
		return nil, err
	}
	if promptPreview.Valid {
		task.PromptPreview = promptPreview.String
	}
	if httpStatus.Valid {
		task.HTTPStatus = int(httpStatus.Int64)
	}
	if result.Valid {
		task.Result = json.RawMessage(result.String)
	}
	if taskError.Valid {
		task.Error = json.RawMessage(taskError.String)
	}
	if storageKeys.Valid {
		_ = json.Unmarshal([]byte(storageKeys.String), &task.StorageKeys)
	}
	task.CreatedAt = createdAt.Unix()
	if completedAt.Valid {
		unix := completedAt.Time.Unix()
		task.CompletedAt = &unix
	}
	task.ExpiresAt = expiresAt.Unix()
	return &task, nil
}

func normalizeRepositoryImageTaskFilter(filter service.ImageTaskHistoryFilter) service.ImageTaskHistoryFilter {
	if filter.Limit <= 0 {
		filter.Limit = 20
	}
	if filter.Limit > 100 {
		filter.Limit = 100
	}
	if filter.Offset < 0 {
		filter.Offset = 0
	}
	return filter
}

func imageTaskHistoryCompletedAt(value *int64) any {
	if value == nil {
		return nil
	}
	return time.Unix(*value, 0).UTC()
}

func serviceImageTaskJSONText(value json.RawMessage) any {
	if len(value) == 0 || !json.Valid(value) {
		return nil
	}
	return string(value)
}

func imageTaskFirstURL(result json.RawMessage) string {
	var payload struct {
		Data []struct {
			URL string `json:"url"`
		} `json:"data"`
	}
	if err := json.Unmarshal(result, &payload); err != nil || len(payload.Data) == 0 {
		return ""
	}
	return payload.Data[0].URL
}

var _ service.ImageTaskHistoryRepository = (*imageTaskHistoryRepository)(nil)
