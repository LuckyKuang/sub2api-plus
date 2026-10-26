-- Operational admission records. Secrets are encrypted with an external,
-- dedicated keyring; ticket state and settlement outbox intent commit together.
-- No FK cascade: deleting/relinking an account must not lose cleanup intent.
CREATE TABLE zhipu_offpeak_tickets (
    id TEXT PRIMARY KEY,
    account_id BIGINT NOT NULL,
    owner_generation TEXT NOT NULL,
    task_id TEXT NOT NULL,
    ticket_id TEXT NOT NULL DEFAULT '',
    state TEXT NOT NULL DEFAULT 'new',
    key_id TEXT NOT NULL,
    encrypted_snapshot BYTEA NOT NULL,
    position INTEGER,
    retired BOOLEAN NOT NULL DEFAULT FALSE,
    closed BOOLEAN NOT NULL DEFAULT FALSE,
    fence BIGINT NOT NULL DEFAULT 0 CHECK (fence >= 0),
    lease_id TEXT NOT NULL DEFAULT '',
    lease_expires_at TIMESTAMPTZ,
    last_used_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    retain_until TIMESTAMPTZ NOT NULL,
    settlement_pending BOOLEAN NOT NULL DEFAULT FALSE,
    next_attempt_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    attempts INTEGER NOT NULL DEFAULT 0 CHECK (attempts >= 0),
    last_error TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CHECK (state IN ('new','uncertain','queued','ready','active','expired','not_found','settling','settled','abandoned')),
    CHECK (NOT closed OR octet_length(encrypted_snapshot) = 0)
);
CREATE UNIQUE INDEX zhipu_offpeak_current_account
    ON zhipu_offpeak_tickets(account_id) WHERE NOT retired AND NOT closed;
CREATE UNIQUE INDEX zhipu_offpeak_remote_ticket
    ON zhipu_offpeak_tickets(account_id, owner_generation, ticket_id) WHERE ticket_id <> '';
CREATE INDEX zhipu_offpeak_settlement_due
    ON zhipu_offpeak_tickets(next_attempt_at, last_used_at) WHERE NOT closed;

CREATE TABLE zhipu_offpeak_request_leases (
    ticket_record_id TEXT NOT NULL REFERENCES zhipu_offpeak_tickets(id) ON DELETE CASCADE,
    request_id TEXT NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY(ticket_record_id, request_id)
);
CREATE INDEX zhipu_offpeak_request_expiry ON zhipu_offpeak_request_leases(expires_at);
