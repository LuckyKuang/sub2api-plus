Sub2API Plus v0.2.10+custom.001

## Highlights

Second release on the official `v0.2.10` baseline. Claude Sonnet 5.5 and
Opus 5.5 are now recognized end to end with a 1,000,000-token context window
and adaptive-thinking defaults, and the admin cyber-risk center gains a user
allowlist that keeps audit evidence for a listed platform user without blocking
it or counting it toward bans. The dashboard can switch the recent-usage trend
between tokens and spending, accounts can show their native Claude reset-credit
status on demand, and composite groups can route over WebSocket with an
account-model ownership check. Plus contracts that this baseline touches are
all preserved: trusted outbound identity precedence, ingress audit ordering,
per-reasoning-effort billing multipliers, the restored GPT-6 fallback pricing
cards, real stream terminal state, and the tool-input de-duplication fix.

The version promotion ships as its own commit ahead of the tag import, so this
release does not itself perform the promotion step.

## Changed

- Models and pricing: `claude-sonnet-5-5` and `claude-opus-5-5` are recognized
  end to end, with the effort catalog, `IsSonnet55`, built-in fallback pricing
  cards, the 1,000,000-token context window, and frontend catalog entries with
  adaptive thinking and effort variants (Sonnet 5.5 defaults to `high`). The
  GPT-6 Sol/Luna fallback cards, the `cache_creation_input_token_cost`
  field-presence rule, and the `>272000` long-context tier are unchanged.
- Forwarded streams: usage normalization for chat completions, Responses, and
  the Anthropic-native paths of the OpenAI gateway, with the mapped model
  written back before the Responses to Anthropic conversion. Claude 5.5
  signed-thinking selections survive the buffered and streamed paths, and tool
  names are rewritten in a single pass without regressing the tool-input
  seed/delta de-duplication.
- Routing and scheduling: composite groups can route over WebSocket and the
  account scheduler applies an account-model ownership veto on top of the
  existing OAuth session-group veto. Rate-limit and billing probes stay
  retired.
- Ingress audit and moderation: the cyber-policy user allowlist and the two
  log-only modes are an additional dimension over the existing action set, and
  the moderation repository applies both the Plus `action <>` filters and the
  upstream `mode <>` filters. Keyword scanning still routes through the Plus
  canonical extraction contract.
- Clients and UI: the admin dashboard can toggle the recent-usage trend between
  tokens and spending, account modals show the Claude native reset-credit
  status, OpenAI groups omit the Codex model catalog, Claude Code-only groups
  expose just the Claude Code client tab, and the model-whitelist mapping
  conflicts in the account modals are fixed.
- Antigravity: compatibility streams stay alive until the first content, without
  affecting Plus output timing, partial usage, or real terminal detection.
- Plus follow-ups: restored the `internal/pkg/claude` imports that the
  auto-merge dropped from the billing and pricing services, corrected the
  generated admin-handler argument order, kept `claude-opus-5-5` selectable in
  the model whitelist catalog, restored the `signature_delta` accumulation so
  Responses keeps Claude 5.5 thinking signatures, and made the moderation
  endpoint map safe when the service is constructed by value.

## Compatibility and migration

- `cyber_policy_user_allowlist` is a new settings key. It is additive; every
  Plus-only settings key is still present and unchanged.
- An OpenAI-compatible client that sets `stream_options.include_usage=true` no
  longer receives a synthesized zero-valued usage chunk for a turn where
  upstream reports no usage. Usage is reported when upstream reports it.
- Sonnet 5.5 defaults to `high` reasoning effort; existing explicit effort and
  thinking settings are unchanged.
- Back up the database before upgrading. This release contains no schema
  migrations.

## Known issues

- The parallel validation lanes exhaust the 8 GB validation container on some
  hosts; run the local matrix with `--serial` there.

## Upstream baseline

Official release: v0.2.10
Official commit: 2f3fed2fdb0787141294cec81487a5df30426f7f
