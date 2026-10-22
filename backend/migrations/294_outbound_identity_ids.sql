-- Random official-client IDs survive application restarts and concurrent replicas.
-- Store only irreversible scope digests; never prompts, raw sessions or credentials.
CREATE TABLE IF NOT EXISTS outbound_identity_ids (
    namespace VARCHAR(64) NOT NULL,
    scope_digest CHAR(64) NOT NULL,
    value VARCHAR(36) NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (namespace, scope_digest)
);
