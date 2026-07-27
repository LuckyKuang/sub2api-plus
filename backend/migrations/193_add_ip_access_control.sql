-- Global IP access control and durable local-login failure state.
-- Rules are deliberately stored outside API-key ACLs: these rules apply before
-- all application routes once the global enforcement setting is enabled.

CREATE TABLE IF NOT EXISTS ip_access_rules (
    id BIGSERIAL PRIMARY KEY,
    ip_or_cidr VARCHAR(64) NOT NULL,
    rule_kind VARCHAR(24) NOT NULL,
    status VARCHAR(16) NOT NULL DEFAULT 'active',
    reason TEXT NOT NULL DEFAULT '',
    failure_count INTEGER NOT NULL DEFAULT 0,
    first_failed_at TIMESTAMPTZ,
    last_failed_at TIMESTAMPTZ,
    blocked_at TIMESTAMPTZ,
    expires_at TIMESTAMPTZ,
    last_seen_at TIMESTAMPTZ,
    hit_count BIGINT NOT NULL DEFAULT 0,
    created_by_user_id BIGINT REFERENCES users(id) ON DELETE SET NULL,
    released_by_user_id BIGINT REFERENCES users(id) ON DELETE SET NULL,
    released_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT ip_access_rules_kind_check CHECK (rule_kind IN ('manual_block', 'auto_block', 'allow')),
    CONSTRAINT ip_access_rules_status_check CHECK (status IN ('active', 'released', 'expired')),
    CONSTRAINT ip_access_rules_failure_count_check CHECK (failure_count >= 0)
);

CREATE UNIQUE INDEX IF NOT EXISTS uq_ip_access_rules_active_kind
    ON ip_access_rules (ip_or_cidr, rule_kind)
    WHERE status = 'active';

CREATE INDEX IF NOT EXISTS idx_ip_access_rules_active_expiry
    ON ip_access_rules (status, expires_at)
    WHERE status = 'active';

CREATE TABLE IF NOT EXISTS ip_login_failure_states (
    normalized_ip VARCHAR(64) PRIMARY KEY,
    failure_count INTEGER NOT NULL DEFAULT 0,
    window_started_at TIMESTAMPTZ NOT NULL,
    last_failed_at TIMESTAMPTZ NOT NULL,
    last_success_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT ip_login_failure_states_failure_count_check CHECK (failure_count >= 0)
);

CREATE INDEX IF NOT EXISTS idx_ip_login_failure_states_last_failed_at
    ON ip_login_failure_states (last_failed_at DESC);
