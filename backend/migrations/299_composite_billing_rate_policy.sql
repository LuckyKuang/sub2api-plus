-- Empty policies preserve all existing selling rates and subscription usage.
ALTER TABLE groups
    ADD COLUMN IF NOT EXISTS billing_rate_policy JSONB NOT NULL DEFAULT '{}'::jsonb,
    ADD COLUMN IF NOT EXISTS billing_policy_version BIGINT NOT NULL DEFAULT 1;

-- Historical usage/order rows intentionally carry no invented pricing evidence.
ALTER TABLE usage_logs
    ADD COLUMN IF NOT EXISTS billing_policy_version BIGINT NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS billing_rate_snapshot JSONB;

ALTER TABLE payment_orders
    ADD COLUMN IF NOT EXISTS pricing_policy_version BIGINT NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS pricing_summary JSONB;

-- Batch jobs already freeze unit prices; retain the resolved customer policy
-- alongside those prices for recovery and delayed settlement. Historical jobs
-- keep an empty record rather than an invented policy version.
ALTER TABLE batch_image_jobs
    ADD COLUMN IF NOT EXISTS billing_rate_snapshot JSONB NOT NULL DEFAULT '{}'::jsonb;
