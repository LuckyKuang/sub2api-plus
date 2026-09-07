UPDATE settings
SET value = 'false', updated_at = NOW()
WHERE key = 'client_disconnect_consecutive_ban_enabled';

UPDATE settings
SET value = CASE
        WHEN BTRIM(value) ~ '^[0-9]+$'
             AND BTRIM(value)::NUMERIC BETWEEN 1 AND 9223372036854775806
            THEN (BTRIM(value)::NUMERIC + 1)::TEXT
        ELSE '2'
    END,
    updated_at = NOW()
WHERE key = 'client_disconnect_consecutive_ban_generation';

ALTER TABLE client_disconnect_risk_events
    ADD COLUMN IF NOT EXISTS session_id VARCHAR(255),
    ADD COLUMN IF NOT EXISTS session_scope VARCHAR(80),
    ADD COLUMN IF NOT EXISTS user_email VARCHAR(255) NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS api_key_name VARCHAR(255);

UPDATE client_disconnect_risk_events AS event
SET user_email = users.email
FROM users
WHERE users.id = event.user_id
  AND event.user_email = '';

UPDATE client_disconnect_risk_events AS event
SET api_key_name = api_keys.name
FROM api_keys
WHERE api_keys.id = event.api_key_id
  AND event.api_key_name IS NULL;

UPDATE client_disconnect_risk_events
SET session_scope = 'legacy'
WHERE session_scope IS NULL OR BTRIM(session_scope) = '';

ALTER TABLE client_disconnect_risk_events
    ALTER COLUMN session_scope SET NOT NULL;

ALTER TABLE client_disconnect_risk_events
    DROP CONSTRAINT IF EXISTS client_disconnect_risk_events_pkey;

ALTER TABLE client_disconnect_risk_events
    ADD CONSTRAINT client_disconnect_risk_events_pkey
        PRIMARY KEY (user_id, session_scope, generation, sequence);

DROP TABLE IF EXISTS client_disconnect_risk_states;

CREATE TABLE client_disconnect_risk_states (
    user_id             BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    session_scope       VARCHAR(80) NOT NULL,
    generation          BIGINT NOT NULL,
    next_sequence       BIGINT NOT NULL DEFAULT 0,
    processed_sequence  BIGINT NOT NULL DEFAULT 0,
    consecutive_count   INT NOT NULL DEFAULT 0,
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (user_id, session_scope)
);

CREATE INDEX IF NOT EXISTS idx_client_disconnect_risk_events_session_accepted
    ON client_disconnect_risk_events(user_id, session_scope, accepted_at DESC);

CREATE INDEX IF NOT EXISTS idx_client_disconnect_risk_events_request_id
    ON client_disconnect_risk_events(request_id);

CREATE INDEX IF NOT EXISTS idx_client_disconnect_risk_events_session_id_accepted
    ON client_disconnect_risk_events(session_id, accepted_at DESC)
    WHERE session_id IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_client_disconnect_risk_events_accepted_order
    ON client_disconnect_risk_events(
        accepted_at DESC, user_id DESC, session_scope DESC, generation DESC, sequence DESC
    );

COMMENT ON COLUMN client_disconnect_risk_events.request_id IS
    'Trusted server request or turn identifier used for lifecycle idempotency.';
COMMENT ON COLUMN client_disconnect_risk_events.session_id IS
    'Sanitized client-provided ingress session identifier; NULL when unavailable.';
COMMENT ON COLUMN client_disconnect_risk_events.session_scope IS
    'Bounded server-derived scope used for independent ordered streak processing.';
COMMENT ON COLUMN client_disconnect_risk_events.user_email IS
    'User account snapshot captured when the upstream request is accepted.';
COMMENT ON COLUMN client_disconnect_risk_events.api_key_name IS
    'API key name snapshot captured when the upstream request is accepted.';
