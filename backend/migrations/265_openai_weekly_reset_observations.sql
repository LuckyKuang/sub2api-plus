-- Retain evidence for off-schedule resets, including repeated resets whose
-- announced next-reset time stays unchanged. Poll leases also cover sources
-- which have not supplied their first usable weekly window yet.
ALTER TABLE openai_oauth_weekly_reset_observations
    ALTER COLUMN reset_at DROP NOT NULL,
    ADD COLUMN IF NOT EXISTS used_percent DOUBLE PRECISION,
    ADD COLUMN IF NOT EXISTS reset_sequence BIGINT NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS pending_reset_at TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS pending_observed_at TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS next_poll_at TIMESTAMPTZ NOT NULL DEFAULT NOW();

ALTER TABLE group_quota_follow_reset_events
    ADD COLUMN IF NOT EXISTS reset_sequence BIGINT NOT NULL DEFAULT 0;

ALTER TABLE group_quota_follow_reset_events
    DROP CONSTRAINT IF EXISTS group_quota_follow_reset_events_identity_unique;
CREATE UNIQUE INDEX IF NOT EXISTS group_quota_follow_reset_events_observation_unique
    ON group_quota_follow_reset_events
        (group_id, config_version, upstream_reset_at, reset_sequence);

COMMENT ON COLUMN groups.quota_reset_source_reset_at IS
    'Last confirmed official default weekly next-reset time; first observation establishes a baseline without resetting subscriptions';
COMMENT ON COLUMN openai_oauth_weekly_reset_observations.reset_sequence IS
    'Monotonic confirmed reset sequence, including resets with an unchanged next-reset time';
