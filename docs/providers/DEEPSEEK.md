# DeepSeek

Sub2API Plus accepts DeepSeek accounts through the OpenAI-compatible gateway
and Claude Code-style Anthropic entry points.

## Empty model mapping

When a DeepSeek account has no `model_mapping`, the scheduler admits only the
supported request names (case-insensitive):

- `deepseek-flash`
- `deepseek-v4.1-flash` (versioned request name; upstream determines support)
- `deepseek-v4-pro`
- `deepseek-v4-flash` (compatibility alias; upstream still accepts it)
- `deepseek-v4-flash-0731` (versioned flash name)
- `deepseek-v4-flash-vision-exp` (compatibility alias)
- `deepseek-v4-pro-0813` (versioned pro name)

The scheduler and model selectors share the [model catalog](../CN_PROVIDER_MODELS.md),
which displays Flash aliases and versioned Flash/Pro names separately.
With no explicit administrator mapping, their spelling is sent upstream unchanged;
the project does not rewrite aliases to `deepseek-flash` or pin that name to V4.1.
The current pricing page recommends `deepseek-flash` and confirms the two V4 Flash
compatibility names; it does not separately document `deepseek-v4.1-flash` as an API ID.

Unknown names fail at scheduling with `404 model_not_found`. They are not
forwarded upstream, so they cannot trigger per-(account, model) cooldown or be
silently rewritten to `deepseek-flash` by DeepSeek.

Claude Code's `ANTHROPIC_MODEL=deepseek-flash[1m]` form is normalized before
the whitelist check, and the `[1m]` suffix is stripped on the DeepSeek outbound
path only. Configured `model_mapping` keeps mapping-first semantics. OpenAI
passthrough accounts still skip mapping admission and send the client model
unchanged.

Identity, ingress audit order, and Plus session/quota accounting are unchanged.
