-- Explicit modes preserve legacy ungrouped keys. Custom membership lives on
-- the key so a scope update is one atomic write independent of usage counters.
ALTER TABLE api_keys
    ADD COLUMN IF NOT EXISTS group_binding_mode VARCHAR(16) NOT NULL DEFAULT 'single',
    ADD COLUMN IF NOT EXISTS aggregate_group_ids JSONB NOT NULL DEFAULT '[]'::jsonb;

ALTER TABLE api_keys ADD CONSTRAINT api_keys_group_binding_check CHECK (
    (group_binding_mode = 'single' AND aggregate_group_ids = '[]'::jsonb)
    OR (group_binding_mode = 'all' AND group_id IS NULL
        AND aggregate_group_ids = '[]'::jsonb AND NOT conversion_enabled
        AND default_client = '' AND default_endpoint = '')
    OR (group_binding_mode = 'custom' AND group_id IS NULL
        AND jsonb_typeof(aggregate_group_ids) = 'array'
        AND aggregate_group_ids <> '[]'::jsonb AND NOT conversion_enabled
        AND default_client = '' AND default_endpoint = '')
);
