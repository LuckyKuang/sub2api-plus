-- Retire arbitrary client-family mappings on native provider and cloud accounts.
-- Keep compatible suppliers/relays, global profiles and runtime declarations.
CREATE OR REPLACE FUNCTION pg_temp.compatible_outbound_identity_key(account_key text) RETURNS boolean
LANGUAGE sql IMMUTABLE AS $$
    SELECT account_key = ANY(ARRAY[
        'openai:apikey', 'openai:upstream',
        'anthropic:apikey', 'anthropic:upstream',
        'gemini:apikey', 'gemini:upstream',
        'grok:apikey', 'grok:upstream',
        'antigravity:upstream', 'typesafe:apikey', 'opencode_go:apikey'
    ]);
$$;

DO $$
DECLARE raw text; config jsonb; original jsonb; account_key text;
BEGIN
    SELECT value INTO raw FROM settings WHERE key = 'outbound_identity' FOR UPDATE;
    IF raw IS NULL THEN RETURN; END IF;
    BEGIN
        config := raw::jsonb;
    EXCEPTION WHEN invalid_text_representation THEN
        RETURN;
    END;
    IF jsonb_typeof(config->'defaults') IS DISTINCT FROM 'object' THEN RETURN; END IF;
    original := config;
    FOR account_key IN SELECT jsonb_object_keys(config->'defaults') LOOP
        IF NOT pg_temp.compatible_outbound_identity_key(account_key) THEN
            config := config #- ARRAY['defaults', account_key];
        END IF;
    END LOOP;
    IF config IS DISTINCT FROM original THEN
        UPDATE settings SET value = config::text, updated_at = NOW() WHERE key = 'outbound_identity';
    END IF;
END;
$$;

-- A foreign candidate must be discarded atomically, not relabelled with a new
-- preset while retaining the old client's UA, version, or companion headers.
UPDATE accounts
SET credentials = credentials - 'outbound_identity', updated_at = NOW()
WHERE platform IN ('openai','anthropic','gemini','grok','antigravity','kimi','zhipu','deepseek','minimax','stepfun','opencode_go','typesafe')
  AND type IN ('oauth','setup-token','apikey','upstream','bedrock','service_account')
  AND NOT pg_temp.compatible_outbound_identity_key(platform || ':' || type)
  AND credentials ? 'outbound_identity'
  AND trim(credentials #>> '{outbound_identity,preset}') IS DISTINCT FROM
      CASE
          WHEN type = 'bedrock' THEN 'claude'
          WHEN platform = 'anthropic' THEN 'claude'
          WHEN platform = 'zhipu' THEN 'zcode'
          WHEN platform = 'minimax' AND type = 'apikey' THEN 'minimax_apikey'
          WHEN platform IN ('gemini','grok','antigravity','kimi','deepseek','minimax','stepfun') THEN platform
          ELSE 'codex'
      END;
