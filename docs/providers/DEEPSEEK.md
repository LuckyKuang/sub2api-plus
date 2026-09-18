# DeepSeek

Sub2API Plus accepts DeepSeek accounts through the OpenAI-compatible gateway
and Claude Code-style Anthropic entry points.

## Empty model mapping

When a DeepSeek account has no `model_mapping`, the scheduler admits only the
official model names (case-insensitive):

- `deepseek-flash`
- `deepseek-v4-pro`
- `deepseek-v4-flash` (compatibility alias; upstream still accepts it)
- `deepseek-v4-flash-vision-exp` (compatibility alias)
- `deepseek-v4-pro-0813` (versioned pro name)

Unknown names fail at scheduling with `404 model_not_found`. They are not
forwarded upstream, so they cannot trigger per-(account, model) cooldown or be
silently rewritten to `deepseek-flash` by DeepSeek.

Claude Code's `ANTHROPIC_MODEL=deepseek-flash[1m]` form is normalized before
the whitelist check, and the `[1m]` suffix is stripped on the DeepSeek outbound
path only. Configured `model_mapping` keeps mapping-first semantics. OpenAI
passthrough accounts still skip mapping admission and send the client model
unchanged.

Identity, ingress audit order, and Plus session/quota accounting are unchanged.