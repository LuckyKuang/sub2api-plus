-- Domestic identity OS declarations are now pinned, not configurable runtime
-- fields. Remove the superseded fields from all persisted source tiers.
CREATE OR REPLACE FUNCTION pg_temp.strip_identity_environment(preset text, headers jsonb) RETURNS jsonb
LANGUAGE plpgsql AS $$
DECLARE name text; names text[];
BEGIN
    IF jsonb_typeof(headers) IS DISTINCT FROM 'object' THEN RETURN headers; END IF;
    CASE preset
        WHEN 'kimi' THEN names := ARRAY['x-msh-device-model', 'x-msh-os-version'];
        WHEN 'zcode' THEN names := ARRAY['x-platform', 'x-os-category', 'x-os-version'];
        ELSE RETURN headers;
    END CASE;
    FOR name IN SELECT jsonb_object_keys(headers) LOOP
        IF lower(trim(name)) = ANY(names) THEN headers := headers - name; END IF;
    END LOOP;
    RETURN headers;
END;
$$;

DO $$
DECLARE raw text; config jsonb; original jsonb; preset text; path text[];
BEGIN
    SELECT value INTO raw FROM settings WHERE key = 'outbound_identity' FOR UPDATE;
    IF raw IS NULL THEN RETURN; END IF;
    BEGIN
        config := raw::jsonb;
    EXCEPTION WHEN invalid_text_representation THEN
        RETURN;
    END;
    original := config;
    FOREACH preset IN ARRAY ARRAY['kimi', 'zcode'] LOOP
        FOREACH path SLICE 1 IN ARRAY ARRAY[['runtime',preset,NULL], ['profiles',preset,'headers']] LOOP
            path := array_remove(path, NULL);
            IF config #> path IS NOT NULL THEN
                config := jsonb_set(config, path, pg_temp.strip_identity_environment(preset, config #> path));
            END IF;
        END LOOP;
    END LOOP;
    IF config IS DISTINCT FROM original THEN
        UPDATE settings SET value = config::text, updated_at = NOW() WHERE key = 'outbound_identity';
    END IF;
END;
$$;

UPDATE accounts
SET credentials = jsonb_set(credentials, '{outbound_identity,headers}',
    pg_temp.strip_identity_environment(credentials #>> '{outbound_identity,preset}', credentials #> '{outbound_identity,headers}')),
    updated_at = NOW()
WHERE credentials #>> '{outbound_identity,preset}' IN ('kimi', 'zcode')
  AND credentials #> '{outbound_identity,headers}' IS DISTINCT FROM
      pg_temp.strip_identity_environment(credentials #>> '{outbound_identity,preset}', credentials #> '{outbound_identity,headers}');
