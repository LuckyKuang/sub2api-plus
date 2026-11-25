-- Declarative request-instruction bindings may coexist on one scope.
-- Only transport capabilities keep an exclusive enabled slot so two packages
-- cannot claim the same platform/account type at once.

DROP INDEX IF EXISTS idx_sub2api_plugin_bindings_one_enabled_scope;

CREATE UNIQUE INDEX IF NOT EXISTS idx_sub2api_plugin_bindings_one_enabled_transport
    ON sub2api_plugin_bindings(capability, platform, account_type)
    WHERE enabled = TRUE
      AND capability IN ('openai.oauth.outbound_transport.v1', 'outbound.transport.v1');
