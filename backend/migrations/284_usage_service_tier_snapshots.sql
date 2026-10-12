-- Keep historical diagnostic values unknown; never backfill from billing.
ALTER TABLE usage_logs
    ADD COLUMN IF NOT EXISTS requested_service_tier VARCHAR(64),
    ADD COLUMN IF NOT EXISTS response_service_tier VARCHAR(64);
COMMENT ON COLUMN usage_logs.requested_service_tier IS 'Final outbound service tier before billing; NULL unrecorded, empty unspecified';
COMMENT ON COLUMN usage_logs.response_service_tier IS 'Upstream tier declaration; NULL unrecorded, empty not returned';
