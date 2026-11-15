-- Durable compensation for async image task terminal-state writes. A completed
-- task previously stayed only in Redis when the PostgreSQL history write failed,
-- so losing the cache lost the result and its object keys. Failed writes are now
-- persisted here (compact record only, no image bytes) and retried until they
-- succeed, across process restarts.
CREATE TABLE IF NOT EXISTS async_image_task_history_retry (
    task_id VARCHAR(64) PRIMARY KEY,
    payload JSONB NOT NULL,
    attempts INTEGER NOT NULL DEFAULT 0,
    next_attempt_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS async_image_task_history_retry_due_idx
    ON async_image_task_history_retry (next_attempt_at);
