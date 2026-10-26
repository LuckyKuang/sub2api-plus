-- Codex versions are maintained in code or explicitly pinned by an administrator.
DELETE FROM settings WHERE key IN (
    'openai_codex_client_version_synced',
    'openai_codex_client_version_synced_checked_at',
    'openai_codex_client_version_sync_error',
    'openai_codex_version_auto_sync_enabled'
);

-- Retire only the OpenAI API-key mapping; compatible upstreams stay configurable.
DO $$
DECLARE raw text; config jsonb;
BEGIN
    SELECT value INTO raw FROM settings WHERE key = 'outbound_identity' FOR UPDATE;
    IF raw IS NULL THEN RETURN; END IF;
    BEGIN
        config := raw::jsonb;
    EXCEPTION WHEN invalid_text_representation THEN
        RETURN;
    END;
    IF jsonb_typeof(config->'defaults') = 'object' AND config->'defaults' ? 'openai:apikey' THEN
        config := config #- ARRAY['defaults','openai:apikey'];
        UPDATE settings SET value = config::text, updated_at = NOW() WHERE key = 'outbound_identity';
    END IF;
END;
$$;

-- Drop a foreign candidate atomically, preserving native UA and credentials.
UPDATE accounts SET credentials = credentials - 'outbound_identity', updated_at = NOW()
WHERE platform = 'openai' AND type = 'apikey'
    AND jsonb_typeof(credentials) = 'object' AND credentials ? 'outbound_identity'
    AND trim(credentials #>> '{outbound_identity,preset}') IS DISTINCT FROM 'codex';
