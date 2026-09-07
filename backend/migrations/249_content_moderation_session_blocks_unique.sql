ALTER TABLE content_moderation_session_blocks
    DROP CONSTRAINT IF EXISTS content_moderation_session_blocks_block_key_key;

DROP INDEX IF EXISTS idx_content_moderation_session_blocks_block_key;

ALTER TABLE content_moderation_session_blocks
    ADD CONSTRAINT content_moderation_session_blocks_block_key_key UNIQUE (block_key);
