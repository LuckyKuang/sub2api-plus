ALTER TABLE usage_logs
    ADD COLUMN IF NOT EXISTS last_token_ms INTEGER;

COMMENT ON COLUMN usage_logs.last_token_ms IS
    'Elapsed ms to the last token-like delta (text/reasoning/tool); NULL = historical or none observed';
