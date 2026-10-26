-- Body-first-data and first assistant text are independent observations.
ALTER TABLE channel_monitors
    ADD COLUMN IF NOT EXISTS stream_probe BOOLEAN NOT NULL DEFAULT false;
ALTER TABLE channel_monitor_request_templates
    ADD COLUMN IF NOT EXISTS stream_probe BOOLEAN NOT NULL DEFAULT false;
ALTER TABLE channel_monitor_histories
    ADD COLUMN IF NOT EXISTS first_text_ms INTEGER CHECK (first_text_ms >= 0),
    ADD COLUMN IF NOT EXISTS stream_probe BOOLEAN;
ALTER TABLE channel_monitor_daily_rollups
    ADD COLUMN IF NOT EXISTS sum_first_text_ms BIGINT NOT NULL DEFAULT 0 CHECK (sum_first_text_ms >= 0),
    ADD COLUMN IF NOT EXISTS count_first_text INTEGER NOT NULL DEFAULT 0 CHECK (count_first_text >= 0);

-- Only replace actually sent stream=true before this migration. Merge/off did
-- not, so do not enable new requests on those configurations. Preserve each
-- monitor's snapshot independently of its template. Historical timings stay NULL.
UPDATE channel_monitors
SET stream_probe = true, body_override = body_override - 'stream'
WHERE provider IN ('openai','grok','kimi','zhipu','deepseek','minimax','stepfun','opencode_go')
  AND check_mode IN ('probe','quota_probe')
  AND body_override_mode = 'replace' AND body_override->'stream' = 'true'::jsonb;
UPDATE channel_monitor_request_templates
SET stream_probe = true, body_override = body_override - 'stream'
WHERE provider IN ('openai','grok','kimi','zhipu','deepseek','minimax','stepfun','opencode_go')
  AND body_override_mode = 'replace' AND body_override->'stream' = 'true'::jsonb;
