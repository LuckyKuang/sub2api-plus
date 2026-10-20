-- Linked ZCode accounts now issue short-lived project access tokens. Retain
-- login and scope, never the obsolete copied project secret or a cached PAT.
UPDATE accounts
SET credentials = (CASE WHEN credentials->>'plan_kind' IN ('individual-coding-plan','team-coding-plan')
                        THEN credentials - 'api_key' ELSE credentials END) - 'off_peak_plan_key',
    updated_at = NOW()
WHERE platform = 'zhipu' AND type = 'oauth'
  AND credentials->>'plan_kind' IN ('individual-coding-plan','team-coding-plan','off-peak');

UPDATE accounts a
SET credentials = jsonb_set(a.credentials, '{header_overrides}',
      COALESCE((SELECT jsonb_object_agg(h.key, h.value)
                FROM jsonb_each(a.credentials->'header_overrides') h
                WHERE lower(h.key) <> 'x-coding-plan-api-key'), '{}'::jsonb)),
    updated_at = NOW()
WHERE a.platform = 'zhipu' AND a.type = 'oauth'
  AND a.credentials->>'plan_kind' IN ('individual-coding-plan','team-coding-plan','off-peak')
  AND jsonb_typeof(a.credentials->'header_overrides') = 'object';
