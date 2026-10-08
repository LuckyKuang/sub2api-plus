-- Official Grok requests preserve tools instead of injecting cache-routing tools.
UPDATE accounts SET extra = extra - 'grok_client_tool_cache_enabled'
WHERE platform = 'grok' AND jsonb_typeof(extra) = 'object' AND extra ? 'grok_client_tool_cache_enabled';
