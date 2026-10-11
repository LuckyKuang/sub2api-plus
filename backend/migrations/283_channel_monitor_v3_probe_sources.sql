ALTER TABLE channel_monitor_v3_facts DROP CONSTRAINT channel_monitor_v3_facts_pkey;
ALTER TABLE channel_monitor_v3_states DROP CONSTRAINT channel_monitor_v3_states_pkey;
DROP INDEX idx_channel_monitor_v3_active_incident;
ALTER TABLE channel_monitor_v3_facts
 ADD COLUMN source_kind text NOT NULL DEFAULT 'request',
 ADD COLUMN scope_id bigint,
 ADD COLUMN probe_status text NOT NULL DEFAULT '',
 ALTER COLUMN group_id DROP NOT NULL;
UPDATE channel_monitor_v3_facts SET scope_id=group_id;
ALTER TABLE channel_monitor_v3_facts ALTER COLUMN scope_id SET NOT NULL;
ALTER TABLE channel_monitor_v3_facts ADD PRIMARY KEY (bucket_start,platform,source_kind,scope_id,model);
ALTER TABLE channel_monitor_v3_states
 ADD COLUMN source_kind text NOT NULL DEFAULT 'request',
 ADD COLUMN scope_id bigint,
 ALTER COLUMN group_id DROP NOT NULL;
UPDATE channel_monitor_v3_states SET scope_id=group_id;
ALTER TABLE channel_monitor_v3_states ALTER COLUMN scope_id SET NOT NULL;
ALTER TABLE channel_monitor_v3_states ADD PRIMARY KEY (platform,source_kind,scope_id,model);
ALTER TABLE channel_monitor_v3_incidents
 ADD COLUMN source_kind text NOT NULL DEFAULT 'request',
 ADD COLUMN scope_id bigint,
 ALTER COLUMN group_id DROP NOT NULL;
UPDATE channel_monitor_v3_incidents SET scope_id=group_id;
ALTER TABLE channel_monitor_v3_incidents ALTER COLUMN scope_id SET NOT NULL;
CREATE UNIQUE INDEX idx_channel_monitor_v3_active_incident ON channel_monitor_v3_incidents (platform,source_kind,scope_id,model) WHERE resolved_at IS NULL;
COMMENT ON TABLE channel_monitor_v3_facts IS 'Anonymous minute request and LLM probe observations; no money, users, credentials or raw errors. Probe latency never enters TTFT. Retained for 31 days.';
