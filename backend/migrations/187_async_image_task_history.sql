CREATE TABLE IF NOT EXISTS async_image_tasks (
    id BIGSERIAL PRIMARY KEY,
    task_id VARCHAR(64) NOT NULL UNIQUE,
    user_id BIGINT NOT NULL,
    api_key_id BIGINT NOT NULL,
    request_type VARCHAR(16) NOT NULL DEFAULT 'generation',
    model VARCHAR(128) NOT NULL DEFAULT '',
    prompt_preview TEXT,
    status VARCHAR(32) NOT NULL DEFAULT 'processing',
    http_status INTEGER,
    image_url TEXT,
    result JSONB,
    error JSONB,
    created_at TIMESTAMPTZ NOT NULL,
    completed_at TIMESTAMPTZ,
    expires_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS async_image_tasks_owner_created_idx
    ON async_image_tasks (user_id, api_key_id, created_at DESC, id DESC);
CREATE INDEX IF NOT EXISTS async_image_tasks_owner_status_created_idx
    ON async_image_tasks (user_id, api_key_id, status, created_at DESC, id DESC);
CREATE INDEX IF NOT EXISTS async_image_tasks_expires_at_idx
    ON async_image_tasks (expires_at);
