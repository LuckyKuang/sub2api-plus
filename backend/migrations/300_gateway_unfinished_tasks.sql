-- Long-running provider tasks outlive HTTP execution leases. Unknown create
-- outcomes intentionally retain capacity until an operator or confirmed
-- terminal observation reconciles them; elapsed time is never completion.
CREATE TABLE gateway_unfinished_tasks (
    reservation_id TEXT PRIMARY KEY,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    api_key_id BIGINT NOT NULL,
    kind TEXT NOT NULL CHECK (kind IN ('batch_image', 'async_image', 'grok_video', 'seedance')),
    task_id TEXT,
    state TEXT NOT NULL DEFAULT 'reserved' CHECK (state IN ('reserved', 'bound', 'uncertain', 'released')),
    reason TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    released_at TIMESTAMPTZ,
    CHECK ((state = 'released') = (released_at IS NOT NULL))
);
CREATE INDEX gateway_unfinished_tasks_active_user_idx ON gateway_unfinished_tasks(user_id) WHERE released_at IS NULL;
CREATE UNIQUE INDEX gateway_unfinished_tasks_owner_task_idx ON gateway_unfinished_tasks(user_id, api_key_id, kind, task_id) WHERE task_id IS NOT NULL;

-- Existing durable unfinished tasks count immediately after upgrade. More than
-- the new cap blocks new submissions without cancelling accepted work.
INSERT INTO gateway_unfinished_tasks (reservation_id,user_id,api_key_id,kind,task_id,state,reason,created_at)
SELECT 'legacy:batch_image:' || batch_id,user_id,api_key_id,'batch_image',batch_id,
       CASE WHEN COALESCE(provider_job_name,'')='' THEN 'uncertain' ELSE 'bound' END,
       'migration_existing_task',created_at
FROM batch_image_jobs WHERE status NOT IN ('completed','failed','cancelled','output_deleted') AND api_key_id IS NOT NULL
AND EXISTS (SELECT 1 FROM users WHERE users.id=batch_image_jobs.user_id);

INSERT INTO gateway_unfinished_tasks (reservation_id,user_id,api_key_id,kind,task_id,state,reason,created_at)
SELECT 'legacy:async_image:' || task_id,user_id,api_key_id,'async_image',task_id,'uncertain','migration_existing_task',created_at
FROM async_image_tasks WHERE status='processing'
AND EXISTS (SELECT 1 FROM users WHERE users.id=async_image_tasks.user_id);
