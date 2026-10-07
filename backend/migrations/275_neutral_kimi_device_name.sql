-- Replace known gateway hostnames only. Legacy OS declarations have no
-- provenance, so preserve them for operator review/reset in identity settings.
-- Keep UUIDs, credentials, explicit neutral names and all other settings intact.
CREATE OR REPLACE FUNCTION pg_temp.neutral_kimi_name(headers jsonb) RETURNS jsonb
LANGUAGE plpgsql AS $$
DECLARE item record;
BEGIN
    IF jsonb_typeof(headers) IS DISTINCT FROM 'object' THEN RETURN headers; END IF;
    FOR item IN SELECT key, value FROM jsonb_each(headers) LOOP
        IF lower(trim(item.key)) = 'x-msh-device-name'
           AND lower(trim(item.value #>> '{}')) IN ('sub2api', 'sub2api-apple') THEN
            headers := jsonb_set(headers, ARRAY[item.key], '"ubuntu"'::jsonb);
        END IF;
    END LOOP;
    RETURN headers;
END;
$$;

DO $$
DECLARE raw text; config jsonb; original jsonb; path text[];
BEGIN
    SELECT value INTO raw FROM settings WHERE key = 'outbound_identity' FOR UPDATE;
    IF raw IS NOT NULL THEN
        BEGIN
            config := raw::jsonb;
        EXCEPTION WHEN invalid_text_representation THEN
            RETURN; -- Preserve malformed legacy configuration for existing fallback.
        END;
        original := config;
        FOREACH path SLICE 1 IN ARRAY ARRAY[['runtime','kimi',NULL], ['profiles','kimi','headers']] LOOP
            -- The rectangular array pads the shorter path; remove NULL elements.
            path := array_remove(path, NULL);
            IF config #> path IS NOT NULL THEN
                config := jsonb_set(config, path, pg_temp.neutral_kimi_name(config #> path));
            END IF;
        END LOOP;
        IF config IS DISTINCT FROM original THEN
            UPDATE settings SET value = config::text, updated_at = NOW() WHERE key = 'outbound_identity';
        END IF;
    END IF;
END;
$$;

UPDATE accounts
SET credentials = jsonb_set(credentials, '{outbound_identity,headers}',
    pg_temp.neutral_kimi_name(credentials #> '{outbound_identity,headers}')),
    updated_at = NOW()
WHERE credentials #>> '{outbound_identity,preset}' = 'kimi'
  AND credentials #> '{outbound_identity,headers}' IS DISTINCT FROM
      pg_temp.neutral_kimi_name(credentials #> '{outbound_identity,headers}');
