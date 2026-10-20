-- Harness persists an anonymous UUID separately from product identity. Backfill
-- credential owners so retries and replicas never fall back to an upstream ID.
UPDATE accounts
SET credentials = jsonb_set(credentials, '{deepseek_harness_user_id}', to_jsonb(gen_random_uuid()::text)),
    updated_at = NOW()
WHERE platform = 'deepseek' AND type IN ('oauth','apikey')
  AND COALESCE(credentials->>'deepseek_harness_user_id','') !~
      '^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$';
