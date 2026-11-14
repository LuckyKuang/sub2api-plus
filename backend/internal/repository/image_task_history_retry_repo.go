package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/service"
)

type imageTaskHistoryRetryRepository struct {
	db *sql.DB
}

func NewImageTaskHistoryRetryRepository(db *sql.DB) service.ImageTaskHistoryRetryRepository {
	return &imageTaskHistoryRetryRepository{db: db}
}

func (r *imageTaskHistoryRetryRepository) Enqueue(ctx context.Context, record *service.ImageTaskRecord) error {
	if r == nil || r.db == nil {
		return fmt.Errorf("async image task history retry database is unavailable")
	}
	if record == nil || record.ID == "" {
		return fmt.Errorf("invalid async image task history retry record")
	}
	payload, err := json.Marshal(record)
	if err != nil {
		return fmt.Errorf("marshal async image task history retry payload: %w", err)
	}
	_, err = r.db.ExecContext(ctx, `
INSERT INTO async_image_task_history_retry (task_id, payload, attempts, next_attempt_at, updated_at)
VALUES ($1, $2::jsonb, 0, NOW(), NOW())
ON CONFLICT (task_id) DO UPDATE SET
    payload = EXCLUDED.payload,
    next_attempt_at = NOW(),
    updated_at = NOW()`, record.ID, string(payload))
	return err
}

func (r *imageTaskHistoryRetryRepository) Due(ctx context.Context, limit int) ([]service.ImageTaskHistoryRetry, error) {
	if r == nil || r.db == nil {
		return nil, fmt.Errorf("async image task history retry database is unavailable")
	}
	if limit <= 0 {
		limit = 20
	}
	rows, err := r.db.QueryContext(ctx, `
SELECT payload::text, attempts FROM async_image_task_history_retry
WHERE next_attempt_at <= NOW()
ORDER BY next_attempt_at ASC
LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	pending := make([]service.ImageTaskHistoryRetry, 0, limit)
	for rows.Next() {
		var payload string
		var attempts int
		if err := rows.Scan(&payload, &attempts); err != nil {
			return nil, err
		}
		var record service.ImageTaskRecord
		if err := json.Unmarshal([]byte(payload), &record); err != nil {
			continue
		}
		pending = append(pending, service.ImageTaskHistoryRetry{Record: &record, Attempts: attempts})
	}
	return pending, rows.Err()
}

func (r *imageTaskHistoryRetryRepository) Delete(ctx context.Context, taskID string) error {
	if r == nil || r.db == nil {
		return fmt.Errorf("async image task history retry database is unavailable")
	}
	_, err := r.db.ExecContext(ctx, `DELETE FROM async_image_task_history_retry WHERE task_id = $1`, taskID)
	return err
}

func (r *imageTaskHistoryRetryRepository) Reschedule(ctx context.Context, taskID string, next time.Time) error {
	if r == nil || r.db == nil {
		return fmt.Errorf("async image task history retry database is unavailable")
	}
	_, err := r.db.ExecContext(ctx, `
UPDATE async_image_task_history_retry
SET attempts = attempts + 1, next_attempt_at = $2, updated_at = NOW()
WHERE task_id = $1`, taskID, next.UTC())
	return err
}

var _ service.ImageTaskHistoryRetryRepository = (*imageTaskHistoryRetryRepository)(nil)
