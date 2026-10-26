-- Samples from a replaced credential/proxy must never confirm another grant's
-- weekly reset or apply a queued reset to a different upstream principal. Empty legacy evidence
-- cannot authorize replay and is discarded on the first verified sample.
ALTER TABLE openai_oauth_weekly_reset_observations
 ADD COLUMN owner_generation text NOT NULL DEFAULT '',
 ADD COLUMN pending_generation text NOT NULL DEFAULT '';
ALTER TABLE group_quota_follow_reset_events
 ADD COLUMN owner_generation text NOT NULL DEFAULT '';
