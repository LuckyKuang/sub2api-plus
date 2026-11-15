-- Image tasks become a user-level history: record the platform so the console can
-- filter by provider without reading the model name, and add the user-scoped
-- indexes that replace key-scoped listing. API-key endpoints keep their existing
-- (user_id, api_key_id) index and ownership semantics.
ALTER TABLE async_image_tasks
    ADD COLUMN IF NOT EXISTS platform VARCHAR(32) NOT NULL DEFAULT '';

-- Historical rows predate the column. Recover the platform only from the stored
-- model name; anything else stays empty and is presented as unknown rather than
-- guessed.
UPDATE async_image_tasks
SET platform = CASE
    WHEN lower(model) LIKE 'grok%' THEN 'grok'
    WHEN lower(model) LIKE 'gpt%'
        OR lower(model) LIKE 'dall-e%'
        OR lower(model) LIKE 'chatgpt%' THEN 'openai'
    ELSE ''
END
WHERE platform = '';

CREATE INDEX IF NOT EXISTS async_image_tasks_user_created_idx
    ON async_image_tasks (user_id, created_at DESC, id DESC);

CREATE INDEX IF NOT EXISTS async_image_tasks_user_platform_created_idx
    ON async_image_tasks (user_id, platform, created_at DESC, id DESC);
