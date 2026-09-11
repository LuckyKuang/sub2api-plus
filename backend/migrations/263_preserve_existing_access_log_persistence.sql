-- A bootstrapped installation has users before migrations run. Fresh databases
-- have none: their application default remains false. Preserve explicit values.
DO $$
DECLARE
    current_value TEXT;
    config JSONB;
BEGIN
    IF NOT EXISTS (SELECT 1 FROM users) THEN RETURN; END IF;
    SELECT value INTO current_value FROM settings WHERE key = 'ops_runtime_log_config';
    BEGIN
        config := COALESCE(NULLIF(current_value, '')::jsonb, '{}'::jsonb);
    EXCEPTION WHEN invalid_text_representation THEN
        RAISE EXCEPTION 'Invalid ops_runtime_log_config JSON, repair before upgrade';
    END;
    IF jsonb_typeof(config) <> 'object' THEN
        RAISE EXCEPTION 'Invalid ops_runtime_log_config object, repair before upgrade';
    END IF;
    IF NOT config ? 'persist_access_logs' THEN
        INSERT INTO settings (key, value, updated_at)
        VALUES ('ops_runtime_log_config', (config || '{"persist_access_logs":true}'::jsonb)::text, NOW())
        ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value, updated_at = EXCLUDED.updated_at;
    END IF;
END $$;
