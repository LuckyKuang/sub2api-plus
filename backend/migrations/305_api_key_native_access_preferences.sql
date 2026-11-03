-- Display/configuration preferences only. Native admission is evaluated for
-- every request; these fields never grant endpoint access or change routing.
ALTER TABLE api_keys ADD COLUMN IF NOT EXISTS default_client VARCHAR(32) NOT NULL DEFAULT '';
ALTER TABLE api_keys ADD COLUMN IF NOT EXISTS default_endpoint VARCHAR(64) NOT NULL DEFAULT '';
