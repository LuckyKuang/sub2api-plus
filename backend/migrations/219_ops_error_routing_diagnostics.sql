-- 219_ops_error_routing_diagnostics.sql
-- Preserve existing SLA business-limit semantics while separately retaining
-- local routing-capacity failures and their sanitized typed diagnostics.

ALTER TABLE ops_error_logs
    ADD COLUMN IF NOT EXISTS is_routing_capacity_limited BOOLEAN NOT NULL DEFAULT FALSE;

ALTER TABLE ops_error_logs
    ADD COLUMN IF NOT EXISTS routing_diagnostics JSONB;

-- Historical routing rows already used the business-limit marker to avoid a
-- breaking SLA series change. Mark them explicitly so the operational Error
-- view can include them without reclassifying old SLA metrics.
UPDATE ops_error_logs
SET is_routing_capacity_limited = TRUE
WHERE COALESCE(error_phase, '') = 'routing'
  AND is_routing_capacity_limited = FALSE;

COMMENT ON COLUMN ops_error_logs.is_routing_capacity_limited IS
    'Local account-selection/capacity failure; independent from business-limit SLA compatibility marker.';

COMMENT ON COLUMN ops_error_logs.routing_diagnostics IS
    'Sanitized typed selection, transport, timeout, and outbound identity-source diagnostics.';
