-- Annotate proxies with egress region metadata (manual administrator input).
--
-- 1. proxies.egress_timezone: egress IANA timezone; drives the Codex
--    environment_context timezone alignment for bound accounts (empty = off,
--    fall through to the account extra / global setting chain).
-- 2. proxies.egress_country: egress country code (ISO 3166-1 alpha-2,
--    empty = off). Named distinctly from the probe-snapshot country fields.
--
-- Runs after 267_purge_unlimited_user_platform_quotas.sql. Both columns default
-- to empty so every existing proxy stays unannotated and behavior is unchanged
-- until an administrator fills the fields in the proxy management UI.

ALTER TABLE proxies
    ADD COLUMN IF NOT EXISTS egress_timezone VARCHAR(64) NOT NULL DEFAULT '';

ALTER TABLE proxies
    ADD COLUMN IF NOT EXISTS egress_country VARCHAR(2) NOT NULL DEFAULT '';
