ALTER TABLE content_moderation_logs
    ADD COLUMN IF NOT EXISTS input_content TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS input_content_truncated BOOLEAN NOT NULL DEFAULT FALSE;
