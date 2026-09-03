ALTER TABLE usage_logs
    ADD COLUMN IF NOT EXISTS completion_status VARCHAR(32) NOT NULL DEFAULT 'unknown',
    ADD COLUMN IF NOT EXISTS usage_source VARCHAR(32) NOT NULL DEFAULT 'unknown';

UPDATE usage_logs
SET completion_status = CASE
        WHEN is_complete IS TRUE THEN 'completed'
        WHEN is_complete IS FALSE THEN 'incomplete'
        ELSE 'unknown'
    END,
    usage_source = CASE
        WHEN is_complete IS TRUE THEN 'upstream_exact'
        WHEN is_complete IS FALSE THEN 'partial'
        ELSE 'unknown'
    END
WHERE completion_status = 'unknown' AND usage_source = 'unknown';

DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'usage_logs_completion_status_check') THEN
        ALTER TABLE usage_logs
            ADD CONSTRAINT usage_logs_completion_status_check
            CHECK (completion_status IN ('unknown', 'completed', 'client_disconnected', 'incomplete'));
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'usage_logs_usage_source_check') THEN
        ALTER TABLE usage_logs
            ADD CONSTRAINT usage_logs_usage_source_check
            CHECK (usage_source IN ('unknown', 'upstream_exact', 'partial', 'estimated', 'reconciled'));
    END IF;
END
$$;

CREATE INDEX IF NOT EXISTS idx_usage_logs_completion_status_created_at
    ON usage_logs(completion_status, created_at DESC);

COMMENT ON COLUMN usage_logs.completion_status IS
    'Explicit terminal state; is_complete remains the compatibility projection.';
COMMENT ON COLUMN usage_logs.usage_source IS
    'upstream_exact, partial, estimated, reconciled, or unknown.';
