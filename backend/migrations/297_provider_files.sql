-- Operational metadata only: no originals, prompts, credentials or proxy secrets.
CREATE TABLE provider_files (
    public_id TEXT PRIMARY KEY CHECK (public_id LIKE 'file-s2a-%'),
    user_id BIGINT NOT NULL,
    api_key_id BIGINT NOT NULL,
    group_id BIGINT NOT NULL,
    platform TEXT NOT NULL CHECK (platform IN ('deepseek', 'minimax')),
    account_id BIGINT NOT NULL,
    principal TEXT NOT NULL,
    request_snapshot_digest TEXT NOT NULL DEFAULT '',
    remote_id TEXT NOT NULL DEFAULT '',
    state TEXT NOT NULL CHECK (state IN ('uploading','ready','uncertain','revoked','delete_pending','deleted')),
    filename TEXT NOT NULL,
    mime_type TEXT NOT NULL,
    purpose TEXT NOT NULL DEFAULT '',
    bytes BIGINT NOT NULL CHECK (bytes > 0 AND bytes <= 67108864),
    content_digest TEXT NOT NULL,
    idempotency_digest TEXT NOT NULL UNIQUE,
    parameter_digest TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL,
    expires_at TIMESTAMPTZ,
    mutation_token TEXT,
    mutation_until TIMESTAMPTZ,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX provider_files_owner_page ON provider_files(user_id, api_key_id, group_id, platform, created_at, public_id);
CREATE INDEX provider_files_account ON provider_files(account_id, principal);
CREATE INDEX provider_files_cleanup ON provider_files(updated_at)
    WHERE platform = 'deepseek' AND remote_id <> '' AND state IN ('uncertain', 'delete_pending');
COMMENT ON TABLE provider_files IS 'Gateway-owned provider file mappings; uncertain uploads are never automatically replayed';
