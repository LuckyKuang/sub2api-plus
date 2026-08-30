ALTER TABLE content_moderation_logs
    ADD COLUMN IF NOT EXISTS moderation_endpoint_id VARCHAR(128) NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS moderation_endpoint_name VARCHAR(255) NOT NULL DEFAULT '';

ALTER TABLE async_image_tasks
    ADD COLUMN IF NOT EXISTS storage_keys JSONB NOT NULL DEFAULT '[]'::jsonb,
    ADD COLUMN IF NOT EXISTS requested_images INT NOT NULL DEFAULT 1,
    ADD COLUMN IF NOT EXISTS actual_images INT NOT NULL DEFAULT 0;

UPDATE async_image_tasks
SET actual_images = jsonb_array_length(result->'data')
WHERE status = 'completed'
  AND actual_images = 0
  AND jsonb_typeof(result->'data') = 'array';
