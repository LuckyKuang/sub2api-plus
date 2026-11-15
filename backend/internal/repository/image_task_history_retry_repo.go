package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/LuckyKuang/sub2api-plus/internal/service"
)

type imageTaskHistoryRetryRepository struct {
	db     *sql.DB
	rdb    *redis.Client
	mu     sync.Mutex
	cursor uint64
}

const imageHistoryRetryRedisKey = "image_task_history_retry"

func NewImageTaskHistoryRetryRepository(db *sql.DB, rdb *redis.Client) service.ImageTaskHistoryRetryRepository {
	return &imageTaskHistoryRetryRepository{db: db, rdb: rdb}
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
	dbCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	_, err = r.db.ExecContext(dbCtx, `
INSERT INTO async_image_task_history_retry (task_id, payload, attempts, next_attempt_at, updated_at)
VALUES ($1, $2::jsonb, 0, NOW(), NOW())
ON CONFLICT (task_id) DO UPDATE SET
    payload = EXCLUDED.payload,
    next_attempt_at = NOW(),
    updated_at = NOW()`, record.ID, string(payload))
	if err != nil && r.rdb != nil {
		// An unavailable history database cannot accept its own compensation
		// row. Keep a separate, non-expiring record until PostgreSQL recovers.
		return r.rdb.HSet(ctx, imageHistoryRetryRedisKey, record.ID, payload).Err()
	}
	return err
}

func (r *imageTaskHistoryRetryRepository) Due(ctx context.Context, limit int) ([]service.ImageTaskHistoryRetry, error) {
	dbCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	pending, dbErr := r.databaseDue(dbCtx, limit)
	cancel()
	if r == nil || r.rdb == nil {
		return pending, dbErr
	}
	// HSCAN is bounded in work per iteration and needs no expiry-prone index.
	r.mu.Lock()
	values, cursor, redisErr := r.rdb.HScan(ctx, imageHistoryRetryRedisKey, r.cursor, "*", int64(limit)).Result()
	if redisErr == nil {
		r.cursor = cursor
	}
	r.mu.Unlock()
	if redisErr != nil {
		if dbErr != nil {
			return nil, errors.Join(dbErr, redisErr)
		}
		return pending, nil
	}
	seen := make(map[string]bool, len(pending))
	for _, item := range pending {
		seen[item.Record.ID] = true
	}
	for i := 1; i < len(values); i += 2 {
		var record service.ImageTaskRecord
		if err := json.Unmarshal([]byte(values[i]), &record); err != nil {
			return nil, err
		}
		if !seen[record.ID] {
			pending = append(pending, service.ImageTaskHistoryRetry{Record: &record})
			seen[record.ID] = true
		}
	}
	return pending, nil
}

func (r *imageTaskHistoryRetryRepository) databaseDue(ctx context.Context, limit int) ([]service.ImageTaskHistoryRetry, error) {
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
	if err != nil {
		return err
	}
	if r.rdb != nil {
		return r.rdb.HDel(ctx, imageHistoryRetryRedisKey, taskID).Err()
	}
	return nil
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
