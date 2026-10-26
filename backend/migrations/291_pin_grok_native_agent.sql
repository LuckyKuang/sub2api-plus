-- Both native Grok OAuth and API-key accounts belong to grok-build. Preserve
-- native account overrides; discard foreign candidates atomically.
UPDATE accounts
SET credentials = credentials - 'outbound_identity', updated_at = NOW()
WHERE platform = 'grok' AND type = 'apikey'
  AND credentials ? 'outbound_identity'
  AND trim(credentials #>> '{outbound_identity,preset}') IS DISTINCT FROM 'grok';

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
    IF jsonb_typeof(config->'defaults') = 'object' AND config->'defaults' ? 'grok:apikey' THEN
        config := config #- ARRAY['defaults','grok:apikey'];
        UPDATE settings SET value = config::text, updated_at = NOW() WHERE key = 'outbound_identity';
    END IF;
END;
$$;
