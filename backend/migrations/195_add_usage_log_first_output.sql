ALTER TABLE usage_logs
    ADD COLUMN IF NOT EXISTS first_output_ms INT,
    ADD COLUMN IF NOT EXISTS first_output_kind VARCHAR(16);

ALTER TABLE usage_logs
    DROP CONSTRAINT IF EXISTS usage_logs_first_output_kind_check;

ALTER TABLE usage_logs
    ADD CONSTRAINT usage_logs_first_output_kind_check
    CHECK (
        first_output_kind IS NULL
        OR first_output_kind IN ('text', 'reasoning', 'tool', 'image', 'audio')
    ) NOT VALID;

-- Pre-195 aggregates contain the old first-event interpretation of
-- first_token_ms and cannot be distinguished row by row. Invalidate only the
-- derived TTFT fields; scheduled aggregation will repopulate eligible buckets
-- from usage_logs using the new semantic marker.
UPDATE ops_metrics_hourly
SET
    ttft_sample_count = 0,
    ttft_p50_ms = NULL,
    ttft_p90_ms = NULL,
    ttft_p95_ms = NULL,
    ttft_p99_ms = NULL,
    ttft_avg_ms = NULL,
    ttft_max_ms = NULL
WHERE
    ttft_sample_count <> 0
    OR ttft_p50_ms IS NOT NULL
    OR ttft_p90_ms IS NOT NULL
    OR ttft_p95_ms IS NOT NULL
    OR ttft_p99_ms IS NOT NULL
    OR ttft_avg_ms IS NOT NULL
    OR ttft_max_ms IS NOT NULL;

UPDATE ops_metrics_daily
SET
    ttft_sample_count = 0,
    ttft_p50_ms = NULL,
    ttft_p90_ms = NULL,
    ttft_p95_ms = NULL,
    ttft_p99_ms = NULL,
    ttft_avg_ms = NULL,
    ttft_max_ms = NULL
WHERE
    ttft_sample_count <> 0
    OR ttft_p50_ms IS NOT NULL
    OR ttft_p90_ms IS NOT NULL
    OR ttft_p95_ms IS NOT NULL
    OR ttft_p99_ms IS NOT NULL
    OR ttft_avg_ms IS NOT NULL
    OR ttft_max_ms IS NOT NULL;

UPDATE ops_system_metrics
SET
    ttft_p50_ms = NULL,
    ttft_p90_ms = NULL,
    ttft_p95_ms = NULL,
    ttft_p99_ms = NULL,
    ttft_avg_ms = NULL,
    ttft_max_ms = NULL
WHERE
    ttft_p50_ms IS NOT NULL
    OR ttft_p90_ms IS NOT NULL
    OR ttft_p95_ms IS NOT NULL
    OR ttft_p99_ms IS NOT NULL
    OR ttft_avg_ms IS NOT NULL
    OR ttft_max_ms IS NOT NULL;
