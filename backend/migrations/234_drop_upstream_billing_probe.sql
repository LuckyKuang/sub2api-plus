-- Drop retired upstream billing-probe extra keys and the global probe settings
-- row. accounts.rate_multiplier is intentionally left unchanged.

WITH updated_accounts AS (
    UPDATE accounts
    SET extra = extra
      - 'upstream_billing_probe'
      - 'upstream_billing_probe_enabled'
      - 'upstream_billing_rate_sync_enabled'
    WHERE extra ?| ARRAY[
        'upstream_billing_probe',
        'upstream_billing_probe_enabled',
        'upstream_billing_rate_sync_enabled'
    ]
    RETURNING id
)
INSERT INTO scheduler_outbox (event_type, account_id)
SELECT 'account_changed', id
FROM updated_accounts;

DELETE FROM settings WHERE key = 'upstream_billing_probe_settings';
