-- Authoritative OpenAI logical-session route records (Codex egress batch 2).
--
-- PostgreSQL is authoritative for the canonical route owner; Redis only
-- accelerates reads and carries the same generation. The table stores no
-- prompt text, credentials, or conversation history: just the tenant domain,
-- an irreversible logical-session digest, the owner account, and the CAS
-- generation/lease state used to commit a migration.
CREATE TABLE IF NOT EXISTS openai_session_routes (
    id BIGSERIAL PRIMARY KEY,
    authz_domain TEXT NOT NULL,
    session_digest TEXT NOT NULL,
    owner_account_id BIGINT NOT NULL,
    generation BIGINT NOT NULL DEFAULT 1,
    route_status TEXT NOT NULL DEFAULT 'active',
    migration_reason TEXT NOT NULL DEFAULT '',
    candidate_account_id BIGINT,
    lease_id TEXT NOT NULL DEFAULT '',
    lease_expires_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT openai_session_routes_domain_digest_key UNIQUE (authz_domain, session_digest),
    CONSTRAINT openai_session_routes_generation_check CHECK (generation > 0),
    CONSTRAINT openai_session_routes_status_check CHECK (route_status IN ('active', 'migrating', 'suspended'))
);

CREATE INDEX IF NOT EXISTS openai_session_routes_owner_account_idx
    ON openai_session_routes (owner_account_id);
