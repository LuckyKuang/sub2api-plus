CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_prompt_audit_events_client_ip_created
    ON prompt_audit_events(client_ip, created_at DESC, id DESC);
