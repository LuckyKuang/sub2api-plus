ALTER TABLE usage_logs ADD COLUMN IF NOT EXISTS timing_version INTEGER NOT NULL DEFAULT 0;
COMMENT ON COLUMN usage_logs.timing_version IS '0: unverified historical timing; 1: strict token-delta timing, including requests with no token output';

ALTER TABLE usage_logs DROP CONSTRAINT IF EXISTS usage_logs_first_output_kind_check;
ALTER TABLE usage_logs ADD CONSTRAINT usage_logs_first_output_kind_check
    CHECK (first_output_kind IS NULL OR first_output_kind IN ('text', 'reasoning', 'tool', 'image', 'audio', 'compaction')) NOT VALID;

-- Derived timing aggregates cannot distinguish the historical semantic mode.
UPDATE ops_metrics_hourly SET ttft_sample_count=0, ttft_p50_ms=NULL, ttft_p90_ms=NULL, ttft_p95_ms=NULL, ttft_p99_ms=NULL, ttft_avg_ms=NULL, ttft_max_ms=NULL WHERE ttft_sample_count <> 0;
UPDATE ops_metrics_daily SET ttft_sample_count=0, ttft_p50_ms=NULL, ttft_p90_ms=NULL, ttft_p95_ms=NULL, ttft_p99_ms=NULL, ttft_avg_ms=NULL, ttft_max_ms=NULL WHERE ttft_sample_count <> 0;
UPDATE ops_system_metrics SET ttft_p50_ms=NULL, ttft_p90_ms=NULL, ttft_p95_ms=NULL, ttft_p99_ms=NULL, ttft_avg_ms=NULL, ttft_max_ms=NULL WHERE ttft_avg_ms IS NOT NULL;
DELETE FROM settings WHERE key = 'openai_ttft_mode';

-- Clear only derived first-token metrics; preserve traffic/cost/duration data.
UPDATE channel_monitor_v2_metrics_1m SET ttft_sum_ms=0, ttft_count=0 WHERE ttft_count <> 0;
UPDATE channel_monitor_v2_user_metrics_1m SET ttft_sum_ms=0, ttft_count=0 WHERE ttft_count <> 0;
UPDATE channel_monitor_v2_metrics_rollup SET ttft_sum_ms=0, ttft_count=0 WHERE ttft_count <> 0;
UPDATE channel_monitor_v2_user_metrics_rollup SET ttft_sum_ms=0, ttft_count=0 WHERE ttft_count <> 0;
DELETE FROM channel_monitor_v2_latency_histograms_1m WHERE metric='ttft';
DELETE FROM channel_monitor_v2_latency_histograms_rollup WHERE metric='ttft';
