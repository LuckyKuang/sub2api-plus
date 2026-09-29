Sub2API Plus v0.2.9+custom.001

## Highlights

First release on the official `v0.2.9` baseline. The integration fixes protocol
conversion, streaming completion, client cancellation, billing, quota
scheduling, and model discovery while keeping every Plus contract: trusted
outbound identity precedence, ingress audit ordering, per-reasoning-effort
billing multipliers, quota semantics, and the restricted Grok fallback.

Channel image prices that are intentionally left blank now inherit the catalog
price instead of being forced to zero, and group model allowlist rules accept
`*` at any position instead of only as a trailing wildcard.

## Changed

- Streams: Antigravity retries an otherwise empty stream or a
  `MALFORMED_FUNCTION_CALL` stream before any irreversible output is
  committed, so downstream sees no duplicate content and no phantom first
  token; Plus audio metering, partial usage, and output timing stay
  authoritative.
- Protocol conversion: base64 PDF/document parts convert to Gemini
  `inlineData`, string `const` stays within its `enum` intersection, explicit
  `thinking: disabled` survives bridged defaults, GPT generation sampling
  parameters are filtered, converted role items are typed as messages, tool
  arguments sent on `content_block_start` are retained, and empty terminal
  text is recovered from already delivered content.
- Identity: `OpenAI-Beta` is honored for Responses multi-agent and Anthropic
  structured output under the Plus non-passthrough policy; inbound UA,
  Originator, Version, and generic overrides still cannot select identity.
- Cancellation: a real client disconnect maps to 499 for uncommitted
  responses while already accepted requests continue their independent
  settlement, billing, and disconnect-risk lifecycle; a lone upstream
  `context.Canceled` on a live client context is no longer classified as a
  client disconnect.
- Billing: account statistics use the account long-context gate, Free Fast
  keeps its zero-cost usage log when a price is missing, Alpha Search only
  reports a billable success on a genuine completed terminal, and the Opus 5.5
  OpenRouter alias is recognized.
- Scheduling and discovery: confirmed no-credit accounts stop re-querying and
  query failures back off, a known future reset time holds the pause, scheduler
  cache projection keeps its fields, and passthrough accounts supplement model
  discovery without bypassing the group allowlist.
- Clients and deployment: CC Switch keeps the Grok/Codex root endpoint, usage
  queries no longer request `/v1/v1/usage`, the Windows Codex model catalog
  uses `~/`, the model plaza shows an independent video multiplier, idle usage
  windows count down from their known reset time, dialog listeners and pending
  searches are cleaned up on unmount, Redis uses the `exec` list command form,
  and the install wizard no longer emits the obsolete `rate_limit` default.
- Plus follow-ups: platform-aware channel pricing reference sync and autofill,
  model-switch clearing of stale prices/intervals/multipliers, new-model
  billing fallback fixes, a Codex client version baseline aligned to official
  v0.158.0, validation-runtime resource cleanup in the push tooling, gofmt
  normalization, production-audit CVE exceptions for the export-only `xlsx`
  usage, and a corrected Codex residency explanation in the admin settings.

## Compatibility and migration

- Channel image prices left blank now inherit the catalog price; keep or set an
  explicit `0` for a free configuration.
- Group model allowlist entries may place `*` at any position (case-insensitive
  full match); `?` and `[]` gain no wildcard meaning. The previous trailing-only
  restriction is removed. Existing databases store the new syntax at runtime;
  the historical one-time migration validation is unchanged.
- Back up the database before upgrading. This release contains no schema
  migrations.

## Known issues

- Grok accounts keep the fixed-point shell identity, base-URL routing, and
  fallback behavior; no known regression.
- The parallel validation lanes exhaust the 8 GB validation container on some
  hosts; run the local matrix with `--serial` there.

## Upstream baseline

Official release: v0.2.9
Official commit: 4c00df2e0183e2c70b7fa8ba45914205e36aad0c
