CREATE OR REPLACE FUNCTION public.enforce_codex_fingerprint_mode_extra()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
DECLARE
    incoming_mode TEXT;
    previous_mode TEXT;
BEGIN
    IF NEW.platform IS DISTINCT FROM 'openai'
        OR NEW.type IS DISTINCT FROM 'oauth'
        OR NEW.parent_account_id IS NOT NULL THEN
        NEW.extra := COALESCE(NEW.extra, '{}'::jsonb) - 'codex_fingerprint_mode';
        RETURN NEW;
    END IF;

    NEW.extra := COALESCE(NEW.extra, '{}'::jsonb);
    IF jsonb_typeof(NEW.extra->'codex_fingerprint_mode') = 'string' THEN
        incoming_mode := btrim(NEW.extra->>'codex_fingerprint_mode');
    END IF;

    IF incoming_mode IN ('off', 'device', 'session', 'full') THEN
        NEW.extra := jsonb_set(
            NEW.extra,
            '{codex_fingerprint_mode}',
            to_jsonb(incoming_mode),
            true
        );
        RETURN NEW;
    END IF;

    IF NEW.extra ? 'codex_fingerprint_mode'
        AND (
            NEW.extra->'codex_fingerprint_mode' = 'null'::jsonb
            OR (
                jsonb_typeof(NEW.extra->'codex_fingerprint_mode') = 'string'
                AND COALESCE(incoming_mode, '') = ''
            )
        ) THEN
        NEW.extra := jsonb_set(
            NEW.extra,
            '{codex_fingerprint_mode}',
            '"device"'::jsonb,
            true
        );
        RETURN NEW;
    END IF;

    IF NEW.extra ? 'codex_fingerprint_mode' THEN
        RAISE EXCEPTION 'codex_fingerprint_mode must be one of off, device, session, full'
            USING ERRCODE = '22023';
    END IF;

    IF TG_OP = 'UPDATE'
        AND OLD.platform = 'openai'
        AND OLD.type = 'oauth'
        AND OLD.parent_account_id IS NULL
        AND jsonb_typeof(OLD.extra->'codex_fingerprint_mode') = 'string' THEN
        previous_mode := btrim(OLD.extra->>'codex_fingerprint_mode');
    END IF;

    IF previous_mode IS NULL
        OR previous_mode NOT IN ('off', 'device', 'session', 'full') THEN
        previous_mode := 'device';
    END IF;
    NEW.extra := jsonb_set(
        NEW.extra,
        '{codex_fingerprint_mode}',
        to_jsonb(previous_mode),
        true
    );
    RETURN NEW;
END;
$$;

UPDATE accounts
SET extra = COALESCE(extra, '{}'::jsonb) - 'codex_fingerprint_mode'
WHERE (
    platform IS DISTINCT FROM 'openai'
    OR type IS DISTINCT FROM 'oauth'
    OR parent_account_id IS NOT NULL
)
  AND COALESCE(extra, '{}'::jsonb) ? 'codex_fingerprint_mode';

UPDATE accounts
SET extra = jsonb_set(
    COALESCE(extra, '{}'::jsonb),
    '{codex_fingerprint_mode}',
    to_jsonb(
        CASE
            WHEN jsonb_typeof(extra->'codex_fingerprint_mode') = 'string'
                AND btrim(extra->>'codex_fingerprint_mode') IN ('off', 'device', 'session', 'full')
                THEN btrim(extra->>'codex_fingerprint_mode')
            ELSE 'device'
        END
    ),
    true
)
WHERE platform = 'openai'
  AND type = 'oauth'
  AND parent_account_id IS NULL
  AND extra->>'codex_fingerprint_mode' IS DISTINCT FROM
      CASE
          WHEN jsonb_typeof(extra->'codex_fingerprint_mode') = 'string'
              AND btrim(extra->>'codex_fingerprint_mode') IN ('off', 'device', 'session', 'full')
              THEN btrim(extra->>'codex_fingerprint_mode')
          ELSE 'device'
      END;

DROP TRIGGER IF EXISTS accounts_enforce_codex_fingerprint_mode_extra ON accounts;
CREATE TRIGGER accounts_enforce_codex_fingerprint_mode_extra
BEFORE INSERT OR UPDATE OF platform, type, extra, parent_account_id
ON accounts
FOR EACH ROW
EXECUTE FUNCTION public.enforce_codex_fingerprint_mode_extra();
