-- Official Codex responses report rollout budget consumption in the
-- response.completed usage payload as `codex_rollout_budget_units` (a JSON
-- number, fractional allowed). Record it per usage log as a reserved billing
-- dimension; NULL means the upstream did not report it.
ALTER TABLE usage_logs
    ADD COLUMN IF NOT EXISTS codex_rollout_budget_units DECIMAL(20, 10);

COMMENT ON COLUMN usage_logs.codex_rollout_budget_units IS
    'Codex-reported rollout budget units from response.completed usage; reserved billing dimension, NULL when unreported';
