-- Add GPT-6 Astra to the front of the factory OpenAI monitor model list.
-- Operator-customized configurations have updated_by set and remain untouched.
UPDATE channel_monitor_v2_config AS config
SET
    platforms = (
        SELECT jsonb_agg(
            CASE
                WHEN platform->>'platform' = 'openai' THEN
                    jsonb_set(
                        platform,
                        '{models}',
                        jsonb_build_array('gpt-6-astra') || (platform->'models'),
                        false
                    )
                ELSE platform
            END
            ORDER BY ordinality
        )
        FROM jsonb_array_elements(config.platforms)
            WITH ORDINALITY AS entries(platform, ordinality)
    ),
    version = version + 1,
    updated_at = NOW()
WHERE id = 1
  AND updated_by IS NULL
  AND jsonb_typeof(platforms) = 'array'
  AND EXISTS (
      SELECT 1
      FROM jsonb_array_elements(config.platforms) AS entries(platform)
      WHERE platform->>'platform' = 'openai'
        AND jsonb_typeof(platform->'models') = 'array'
        AND platform->'models' @> '["gpt-5.6-sol"]'::jsonb
        AND NOT (platform->'models' @> '["gpt-6-astra"]'::jsonb)
  );
