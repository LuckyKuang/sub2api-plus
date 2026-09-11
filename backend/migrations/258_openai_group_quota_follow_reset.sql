ALTER TABLE groups
    ADD COLUMN IF NOT EXISTS quota_reset_source_account_id BIGINT,
    ADD COLUMN IF NOT EXISTS quota_reset_source_account_name VARCHAR(100) NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS quota_reset_source_reset_at TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS quota_reset_include_monthly BOOLEAN NOT NULL DEFAULT FALSE,
    ADD COLUMN IF NOT EXISTS quota_reset_config_version BIGINT NOT NULL DEFAULT 0;

COMMENT ON COLUMN groups.quota_reset_source_account_id IS 'OpenAI OAuth account whose raw weekly reset_at drives subscription quota resets; not an FK so deleted sources remain diagnosable';
COMMENT ON COLUMN groups.quota_reset_source_reset_at IS 'Last accepted raw upstream weekly reset_at; the first value is a non-resetting baseline';
COMMENT ON COLUMN groups.quota_reset_config_version IS 'Monotonic source configuration generation used to reject obsolete events';

ALTER TABLE user_subscriptions
    ADD COLUMN IF NOT EXISTS quota_follow_reset_event_id BIGINT NOT NULL DEFAULT 0;

CREATE TABLE IF NOT EXISTS openai_oauth_weekly_reset_observations (
    account_id BIGINT PRIMARY KEY,
    reset_at TIMESTAMPTZ NOT NULL,
    observed_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS group_quota_follow_reset_events (
    id BIGSERIAL PRIMARY KEY,
    group_id BIGINT NOT NULL,
    source_account_id BIGINT NOT NULL,
    config_version BIGINT NOT NULL,
    upstream_reset_at TIMESTAMPTZ NOT NULL,
    effective_at TIMESTAMPTZ NOT NULL,
    include_monthly BOOLEAN NOT NULL DEFAULT FALSE,
    status VARCHAR(20) NOT NULL DEFAULT 'pending',
    processed_at TIMESTAMPTZ,
    affected_subscriptions INTEGER NOT NULL DEFAULT 0,
    last_error VARCHAR(500) NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT group_quota_follow_reset_events_status_check
        CHECK (status IN ('pending', 'completed', 'skipped')),
    CONSTRAINT group_quota_follow_reset_events_identity_unique
        UNIQUE (group_id, config_version, upstream_reset_at)
);

CREATE INDEX IF NOT EXISTS group_quota_follow_reset_events_pending_idx
    ON group_quota_follow_reset_events (id)
    WHERE status = 'pending';

CREATE INDEX IF NOT EXISTS group_quota_follow_reset_events_group_idx
    ON group_quota_follow_reset_events (group_id, id DESC)
    WHERE status IN ('pending', 'completed');

CREATE INDEX IF NOT EXISTS groups_quota_reset_source_idx
    ON groups (quota_reset_source_account_id)
    WHERE deleted_at IS NULL AND quota_reset_source_account_id IS NOT NULL;

CREATE INDEX IF NOT EXISTS user_subscriptions_quota_follow_event_idx
    ON user_subscriptions (group_id, quota_follow_reset_event_id)
    WHERE deleted_at IS NULL;
