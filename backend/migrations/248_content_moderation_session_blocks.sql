CREATE TABLE IF NOT EXISTS content_moderation_session_blocks (
    id              BIGSERIAL PRIMARY KEY,
    block_key       VARCHAR(64) NOT NULL,
    session_id      VARCHAR(255) NOT NULL,
    user_id         BIGINT REFERENCES users(id) ON DELETE SET NULL,
    api_key_id      BIGINT REFERENCES api_keys(id) ON DELETE SET NULL,
    request_id      VARCHAR(128) NOT NULL DEFAULT '',
    endpoint        VARCHAR(128) NOT NULL DEFAULT '',
    protocol        VARCHAR(64) NOT NULL DEFAULT '',
    model           VARCHAR(255) NOT NULL DEFAULT '',
    highest_category VARCHAR(64) NOT NULL DEFAULT '',
    highest_score   DECIMAL(8, 6) NOT NULL DEFAULT 0,
    expires_at      TIMESTAMPTZ NOT NULL,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_content_moderation_session_blocks_block_key
    ON content_moderation_session_blocks (block_key);

CREATE INDEX IF NOT EXISTS idx_content_moderation_session_blocks_expires_at
    ON content_moderation_session_blocks (expires_at);

CREATE INDEX IF NOT EXISTS idx_content_moderation_session_blocks_session_id
    ON content_moderation_session_blocks (session_id);

CREATE INDEX IF NOT EXISTS idx_content_moderation_session_blocks_user_id
    ON content_moderation_session_blocks (user_id);
