-- Correct only historical local 403 refusals with no account/upstream evidence.
-- Malformed/truncated payloads and ambiguous/provider records remain untouched.
-- Endpoint names were historically derived before selection and are not upstream evidence.
-- Preserve timestamps, response bodies, business-limit/SLA flags and billing.
DO $$
DECLARE
    row_record record;
    payload jsonb;
    local_code text;
BEGIN
    FOR row_record IN
        SELECT id, error_body
        FROM ops_error_logs
        WHERE status_code = 403
          AND error_phase = 'internal' AND error_type = 'api_error'
          AND error_owner = 'platform' AND error_source = 'gateway'
          AND account_id IS NULL
          AND COALESCE(upstream_status_code, 0) = 0
          AND COALESCE(upstream_error_message, '') = ''
          AND COALESCE(upstream_error_detail, '') = ''
          AND (upstream_errors IS NULL OR upstream_errors = '[]'::jsonb)
          AND COALESCE(upstream_model, '') = ''
          AND error_body IS NOT NULL
    LOOP
        BEGIN
            payload := row_record.error_body::jsonb;
        EXCEPTION WHEN invalid_text_representation THEN
            CONTINUE;
        END;
        local_code := COALESCE(NULLIF(payload #>> '{error,code}', ''), payload #>> '{error,type}');
        IF local_code IN ('content_policy_violation', 'session_blocked_by_content_policy', 'prompt_guard_blocked')
           AND payload #>> '{error,type}' IN (local_code, 'api_error', 'permission_error')
           AND jsonb_typeof(payload -> 'error') = 'object'
        THEN
            UPDATE ops_error_logs
            SET error_phase = 'request', error_type = local_code,
                error_owner = 'client', error_source = 'client_request'
            WHERE id = row_record.id;
        END IF;
    END LOOP;
END $$;
