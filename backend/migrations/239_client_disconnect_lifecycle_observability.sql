ALTER TABLE client_disconnect_risk_events
    ADD COLUMN IF NOT EXISTS completion_status VARCHAR(32),
    ADD COLUMN IF NOT EXISTS usage_source VARCHAR(32),
    ADD COLUMN IF NOT EXISTS usage_missing BOOLEAN NOT NULL DEFAULT TRUE;

DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'client_disconnect_risk_events_completion_status_check') THEN
        ALTER TABLE client_disconnect_risk_events
            ADD CONSTRAINT client_disconnect_risk_events_completion_status_check
            CHECK (completion_status IS NULL OR completion_status IN (
                'completed', 'client_disconnected', 'upstream_failed',
                'upstream_timeout', 'usage_missing'
            ));
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'client_disconnect_risk_events_usage_source_check') THEN
        ALTER TABLE client_disconnect_risk_events
            ADD CONSTRAINT client_disconnect_risk_events_usage_source_check
            CHECK (usage_source IS NULL OR usage_source IN (
                'upstream_exact', 'partial', 'estimated', 'reconciled'
            ));
    END IF;
END
$$;

CREATE INDEX IF NOT EXISTS idx_client_disconnect_risk_events_usage_missing
    ON client_disconnect_risk_events(accepted_at DESC)
    WHERE usage_missing = TRUE;

COMMENT ON COLUMN client_disconnect_risk_events.completion_status IS
    'Explicit accepted-request terminal status; NULL while pending or historical.';
COMMENT ON COLUMN client_disconnect_risk_events.usage_source IS
    'Quality/source of usage used for settlement; NULL when usage is unavailable.';
COMMENT ON COLUMN client_disconnect_risk_events.usage_missing IS
    'True when an accepted request reached its terminal state without usable usage.';
