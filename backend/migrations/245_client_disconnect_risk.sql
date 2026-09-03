INSERT INTO settings (key, value, updated_at)
VALUES
    ('client_disconnect_consecutive_ban_enabled', 'true', NOW()),
    ('client_disconnect_consecutive_ban_threshold', '10', NOW()),
    ('client_disconnect_consecutive_ban_generation', '1', NOW())
ON CONFLICT (key) DO NOTHING;

CREATE TABLE IF NOT EXISTS client_disconnect_risk_states (
    user_id             BIGINT PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    generation          BIGINT NOT NULL,
    next_sequence       BIGINT NOT NULL DEFAULT 0,
    processed_sequence  BIGINT NOT NULL DEFAULT 0,
    consecutive_count   INT NOT NULL DEFAULT 0,
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS client_disconnect_risk_events (
    user_id             BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    generation          BIGINT NOT NULL,
    sequence            BIGINT NOT NULL,
    request_id          VARCHAR(255) NOT NULL,
    api_key_id          BIGINT REFERENCES api_keys(id) ON DELETE SET NULL,
    protocol            VARCHAR(64) NOT NULL DEFAULT '',
    outcome             VARCHAR(32) NOT NULL DEFAULT 'pending',
    threshold           INT,
    enforce             BOOLEAN,
    consecutive_after   INT,
    auto_banned         BOOLEAN NOT NULL DEFAULT FALSE,
    accepted_at         TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    finalized_at        TIMESTAMPTZ,
    PRIMARY KEY (user_id, generation, sequence),
    UNIQUE (user_id, generation, request_id),
    CONSTRAINT client_disconnect_risk_events_outcome_check
        CHECK (outcome IN ('pending', 'completed', 'client_disconnected', 'neutral')),
    CONSTRAINT client_disconnect_risk_events_threshold_check
        CHECK (threshold IS NULL OR threshold BETWEEN 1 AND 1000)
);

CREATE INDEX IF NOT EXISTS idx_client_disconnect_risk_events_user_accepted
    ON client_disconnect_risk_events(user_id, accepted_at DESC);

CREATE INDEX IF NOT EXISTS idx_client_disconnect_risk_events_disconnects
    ON client_disconnect_risk_events(accepted_at DESC)
    WHERE outcome = 'client_disconnected';

CREATE OR REPLACE FUNCTION reset_client_disconnect_risk_on_user_trust_change()
RETURNS TRIGGER AS $$
BEGIN
    IF (OLD.status IS DISTINCT FROM 'active' AND NEW.status = 'active')
       OR (OLD.role IS DISTINCT FROM 'admin' AND NEW.role = 'admin') THEN
        UPDATE client_disconnect_risk_states
        SET processed_sequence = next_sequence,
            consecutive_count = 0,
            updated_at = NOW()
        WHERE user_id = NEW.id;
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS trg_reset_client_disconnect_risk_on_user_trust_change ON users;
CREATE TRIGGER trg_reset_client_disconnect_risk_on_user_trust_change
AFTER UPDATE OF status, role ON users
FOR EACH ROW
EXECUTE FUNCTION reset_client_disconnect_risk_on_user_trust_change();
