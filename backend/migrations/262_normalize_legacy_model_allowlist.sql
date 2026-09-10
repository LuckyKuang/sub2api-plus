-- Preserve the old display-only empty-list behavior during this one-time upgrade.
-- Invalid nonempty policies require operator repair; never invent a wildcard.
DO $$
DECLARE
    row_data RECORD;
    entry JSONB;
    model TEXT;
    normalized JSONB;
    seen TEXT[];
BEGIN
    FOR row_data IN SELECT id, model_allowlist FROM groups LOOP
        IF row_data.model_allowlist = '{}'::jsonb THEN
            CONTINUE;
        END IF;
        IF jsonb_typeof(row_data.model_allowlist) <> 'object'
           OR (row_data.model_allowlist ? 'enabled' AND jsonb_typeof(row_data.model_allowlist->'enabled') <> 'boolean')
           OR (row_data.model_allowlist ? 'models' AND jsonb_typeof(row_data.model_allowlist->'models') NOT IN ('array', 'null')) THEN
            RAISE EXCEPTION 'Invalid legacy model allowlist for group %, repair before upgrade', row_data.id;
        END IF;
        normalized := '[]'::jsonb;
        seen := ARRAY[]::TEXT[];
        FOR entry IN SELECT value FROM jsonb_array_elements(COALESCE(NULLIF(row_data.model_allowlist->'models', 'null'::jsonb), '[]'::jsonb)) LOOP
            IF jsonb_typeof(entry) <> 'string' THEN
                RAISE EXCEPTION 'Invalid legacy model allowlist entry for group %, repair before upgrade', row_data.id;
            END IF;
            model := regexp_replace(entry #>> '{}', '^\s+|\s+$', '', 'g');
            IF model = '' THEN CONTINUE; END IF;
            IF position('*' IN regexp_replace(model, '\*$', '')) > 0 THEN
                RAISE EXCEPTION 'Invalid legacy model allowlist wildcard for group %, repair before upgrade', row_data.id;
            END IF;
            IF NOT lower(model) = ANY(seen) THEN
                normalized := normalized || jsonb_build_array(model);
                seen := array_append(seen, lower(model));
            END IF;
        END LOOP;
        IF normalized = '[]'::jsonb AND row_data.model_allowlist->'enabled' = 'true'::jsonb THEN
            RAISE NOTICE 'Disabled legacy empty model allowlist for group %', row_data.id;
        END IF;
        UPDATE groups SET model_allowlist = row_data.model_allowlist || jsonb_build_object(
            'enabled', COALESCE((row_data.model_allowlist->>'enabled')::boolean, false) AND normalized <> '[]'::jsonb,
            'models', normalized)
        WHERE id = row_data.id;
    END LOOP;
END $$;
