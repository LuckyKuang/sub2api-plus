ALTER TABLE prompt_audit_jobs
    ADD COLUMN IF NOT EXISTS client_ip VARCHAR(64) NOT NULL DEFAULT '';

ALTER TABLE prompt_audit_events
    ADD COLUMN IF NOT EXISTS client_ip VARCHAR(64) NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS prompt_length INT NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS message_count INT NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS execution_mode VARCHAR(32) NOT NULL DEFAULT 'async_audit',
    ADD COLUMN IF NOT EXISTS queue_delay_ms INT,
    ADD COLUMN IF NOT EXISTS input_limit INT,
    ADD COLUMN IF NOT EXISTS matched_chunk_index INT,
    ADD COLUMN IF NOT EXISTS full_prompt_truncated BOOLEAN NOT NULL DEFAULT FALSE;

UPDATE prompt_audit_events AS event
SET prompt_length = job.prompt_length,
    message_count = job.message_count,
    execution_mode = job.execution_mode,
    client_ip = job.client_ip
FROM prompt_audit_jobs AS job
WHERE job.id = event.job_id;

UPDATE prompt_audit_events
SET full_prompt_truncated = TRUE
WHERE input_limit IS NULL
  AND (
      prompt_length > CHAR_LENGTH(full_prompt) OR
      (
          prompt_length >= 65537 AND CHAR_LENGTH(full_prompt) = 65537 AND
          RIGHT(full_prompt, 1) = '…'
      )
  );

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint
        WHERE conname = 'chk_prompt_audit_events_observability'
          AND conrelid = 'prompt_audit_events'::regclass
    ) THEN
        ALTER TABLE prompt_audit_events
            ADD CONSTRAINT chk_prompt_audit_events_observability
            CHECK (
                prompt_length >= 0 AND message_count >= 0 AND
                execution_mode IN ('async_audit', 'blocking') AND
                (queue_delay_ms IS NULL OR queue_delay_ms >= 0) AND
                (input_limit IS NULL OR input_limit >= 128) AND
                (matched_chunk_index IS NULL OR (
                    matched_chunk_index >= 1 AND matched_chunk_index <= chunk_total
                ))
            );
    END IF;
END $$;
