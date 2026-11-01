-- Preserve dispatch evidence independently from inbound user_agent and usage accuracy.
-- No historical inference/backfill: NULL means no runtime observation.
ALTER TABLE usage_logs ADD COLUMN IF NOT EXISTS outbound_metadata JSONB;
ALTER TABLE usage_logs DROP CONSTRAINT IF EXISTS usage_logs_outbound_metadata_check;
ALTER TABLE usage_logs ADD CONSTRAINT usage_logs_outbound_metadata_check CHECK (
    outbound_metadata IS NULL OR (
        jsonb_typeof(outbound_metadata) = 'object'
        AND outbound_metadata->>'transport' IS NOT NULL
        AND outbound_metadata->>'transport' IN ('native', 'codex_cli_bridge', 'codex_cli_task')
        AND outbound_metadata->>'evidence' IS NOT NULL
        AND ((outbound_metadata->>'transport' IN ('native', 'codex_cli_bridge') AND outbound_metadata->>'evidence' = 'gateway_send')
             OR (outbound_metadata->>'transport' = 'codex_cli_task' AND outbound_metadata->>'evidence' = 'cli_process'))
    )
);
