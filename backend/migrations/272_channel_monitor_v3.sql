-- V3 records terminal request outcomes, independently of billing and V2 settings.
CREATE TABLE IF NOT EXISTS channel_monitor_v3_config (
    id SMALLINT PRIMARY KEY DEFAULT 1 CHECK (id = 1),
    version INTEGER NOT NULL DEFAULT 1,
    config JSONB NOT NULL DEFAULT '{"minimum_samples":5,"warning_error_rate":0.05,"outage_error_rate":0.9,"warning_ttft_ms":5000,"abnormal_windows":2,"recovery_windows":3}',
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
INSERT INTO channel_monitor_v3_config (id) VALUES (1) ON CONFLICT DO NOTHING;

CREATE TABLE IF NOT EXISTS channel_monitor_v3_facts (
    bucket_start TIMESTAMPTZ NOT NULL,
    platform TEXT NOT NULL,
    group_id BIGINT NOT NULL,
    model TEXT NOT NULL,
    success_requests BIGINT NOT NULL CHECK (success_requests >= 0),
    failed_requests BIGINT NOT NULL CHECK (failed_requests >= 0),
    last_request_at TIMESTAMPTZ NOT NULL,
    ttft_counts BIGINT[] NOT NULL CHECK (cardinality(ttft_counts) = 16),
    PRIMARY KEY (bucket_start, platform, group_id, model)
);
CREATE INDEX IF NOT EXISTS idx_channel_monitor_v3_facts_scope_time ON channel_monitor_v3_facts (group_id, bucket_start DESC);

CREATE TABLE IF NOT EXISTS channel_monitor_v3_states (
    platform TEXT NOT NULL,
    group_id BIGINT NOT NULL,
    model TEXT NOT NULL,
    data JSONB NOT NULL,
    PRIMARY KEY (platform, group_id, model)
);

CREATE TABLE IF NOT EXISTS channel_monitor_v3_incidents (
    id TEXT PRIMARY KEY,
    platform TEXT NOT NULL,
    group_id BIGINT NOT NULL,
    model TEXT NOT NULL,
    started_at TIMESTAMPTZ NOT NULL,
    resolved_at TIMESTAMPTZ,
    data JSONB NOT NULL
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_channel_monitor_v3_active_incident ON channel_monitor_v3_incidents (platform, group_id, model) WHERE resolved_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_channel_monitor_v3_incidents_scope_time ON channel_monitor_v3_incidents (group_id, started_at DESC);

CREATE TABLE IF NOT EXISTS channel_monitor_v3_watermark (
    id SMALLINT PRIMARY KEY DEFAULT 1 CHECK (id = 1),
    data_through TIMESTAMPTZ
);
INSERT INTO channel_monitor_v3_watermark (id) VALUES (1) ON CONFLICT DO NOTHING;

COMMENT ON TABLE channel_monitor_v3_facts IS 'Anonymous minute terminal outcomes; no costs, users, credentials, raw errors or active probes. Retained for 31 days.';
COMMENT ON TABLE channel_monitor_v3_incidents IS 'Server-scoped automatic incident transitions. Silence never resolves an incident.';
