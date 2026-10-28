-- Provider-complete Batch phases must not keep unfinished-task capacity while
-- indexing, settlement or worker recovery is delayed. Match the full owner and
-- task identity: a reused task ID cannot release another key, user or kind.
-- A local failed/cancelled status, billing amount or elapsed time does not prove
-- provider termination and must never release an unknown create outcome.
UPDATE gateway_unfinished_tasks AS reservation
SET state = 'released',
    reason = 'migration_provider_terminal',
    released_at = NOW(),
    updated_at = NOW()
FROM batch_image_jobs AS job
WHERE reservation.released_at IS NULL
  AND reservation.kind = 'batch_image'
  AND reservation.user_id = job.user_id
  AND reservation.api_key_id = job.api_key_id
  AND reservation.task_id = job.batch_id
  AND job.status IN ('indexing', 'settling', 'completed', 'output_deleted');
