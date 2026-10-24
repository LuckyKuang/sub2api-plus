-- Model exclusions are read-time policy over retained per-model facts. Preserve
-- all traffic/history; only supply explicit defaults for old platform entries.
ALTER TABLE channel_monitor_v2_config ALTER COLUMN platforms SET DEFAULT
  '[{"platform":"anthropic","enabled":true,"models":[],"model_display_mode":"all","excluded_models":[]},{"platform":"openai","enabled":true,"models":[],"model_display_mode":"all","excluded_models":[]},{"platform":"grok","enabled":true,"models":[],"model_display_mode":"all","excluded_models":[]},{"platform":"kiro","enabled":true,"models":[],"model_display_mode":"all","excluded_models":[]},{"platform":"gemini","enabled":true,"models":[],"model_display_mode":"all","excluded_models":[]},{"platform":"antigravity","enabled":true,"models":[],"model_display_mode":"all","excluded_models":[]}]'::jsonb;

UPDATE channel_monitor_v2_config AS config
SET platforms = (
    SELECT jsonb_agg(
      CASE WHEN jsonb_typeof(platform) = 'object' THEN
        platform || jsonb_build_object(
          'excluded_models', COALESCE(NULLIF(platform->'excluded_models', 'null'::jsonb), '[]'::jsonb),
          'model_display_mode', COALESCE(NULLIF(platform->>'model_display_mode', ''),
            CASE WHEN jsonb_typeof(platform->'models') = 'array' AND platform->'models' <> '[]'::jsonb
              THEN 'selected' ELSE 'all' END)
        )
      ELSE platform END ORDER BY ordinality
    ) FROM jsonb_array_elements(config.platforms) WITH ORDINALITY AS entries(platform, ordinality)
  ),
  version = version + 1,
  updated_at = NOW()
WHERE jsonb_typeof(platforms) = 'array'
  AND EXISTS (
    SELECT 1 FROM jsonb_array_elements(config.platforms) AS entries(platform)
    WHERE jsonb_typeof(platform) = 'object'
      AND (platform->'excluded_models' IS NULL OR platform->'excluded_models' = 'null'::jsonb
        OR COALESCE(platform->>'model_display_mode', '') = '')
  );
