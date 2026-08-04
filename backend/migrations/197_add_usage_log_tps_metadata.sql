ALTER TABLE usage_logs
    ADD COLUMN IF NOT EXISTS audio_output_tokens INTEGER NOT NULL DEFAULT 0;

ALTER TABLE usage_logs
    ADD COLUMN IF NOT EXISTS is_complete BOOLEAN;

COMMENT ON COLUMN usage_logs.is_complete IS
    'Whether upstream output completed normally; NULL means historical or unknown';
