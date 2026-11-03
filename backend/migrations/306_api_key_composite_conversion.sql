-- Conversion is explicit opt-in. Existing saved endpoint preferences must never
-- enable protocol conversion or restrict ingress for an existing key.
ALTER TABLE api_keys ADD COLUMN IF NOT EXISTS conversion_enabled BOOLEAN NOT NULL DEFAULT FALSE;
