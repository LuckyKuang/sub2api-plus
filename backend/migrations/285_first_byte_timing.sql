-- First actual upstream response-body data, independent from strict tokens.
-- Historical rows remain unobserved; zero milliseconds is a valid observation.
ALTER TABLE usage_logs ADD COLUMN IF NOT EXISTS first_byte_ms INTEGER
    CHECK (first_byte_ms >= 0);
ALTER TABLE channel_monitor_histories ADD COLUMN IF NOT EXISTS first_byte_ms INTEGER
    CHECK (first_byte_ms >= 0);
ALTER TABLE channel_monitor_daily_rollups
    ADD COLUMN IF NOT EXISTS sum_first_byte_ms BIGINT NOT NULL DEFAULT 0 CHECK (sum_first_byte_ms >= 0),
    ADD COLUMN IF NOT EXISTS count_first_byte INTEGER NOT NULL DEFAULT 0 CHECK (count_first_byte >= 0);
