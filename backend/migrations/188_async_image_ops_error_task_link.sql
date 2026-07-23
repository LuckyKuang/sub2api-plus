-- Link the one terminal failure of an asynchronous image task to its
-- client-visible Ops error record. Regular/synchronous requests leave this
-- value NULL and are unaffected by the partial uniqueness constraint.
ALTER TABLE ops_error_logs
    ADD COLUMN IF NOT EXISTS async_task_id VARCHAR(64);

CREATE UNIQUE INDEX IF NOT EXISTS idx_ops_error_logs_async_task_id_unique
    ON ops_error_logs (async_task_id)
    WHERE async_task_id IS NOT NULL;

COMMENT ON COLUMN ops_error_logs.async_task_id IS
    'Async image task ID. Non-null values are unique so a terminal task failure has one Ops error record.';
