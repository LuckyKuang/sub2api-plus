Sub2API Plus v0.2.8+custom.001

## Highlights

Second Plus release on the official `v0.2.8` baseline. It keeps Plus identity,
ingress audit, session/quota accounting, proxy egress metadata, and retired
billing probes, while importing the official GPT-6 Sol/Luna, Claude Opus 5.5,
and Grok 4.7 model support, the OpenCode Go official usage window with
automatic refresh, configurable reasoning-effort billing multipliers, automatic
Claude Code client version synchronization, simple-mode API key consumption
windows, monthly backup archives, offline affiliate withdrawal registration,
rolling log retention, Codex credits display, and independent TypeSafe content
audit profiles. Plus adds three rounds of Codex OAuth outbound alignment with
the official `codex-rs` client on top of this baseline.

## Changed

- New model support: GPT-6 Sol, GPT-6 Luna, Claude Opus 5.5, and Grok 4.7.
- OpenCode Go usage window: official quota query, automatic refresh, same-key group sharing, manual query, and account list/usage-cell balance badges (7d/1m); the `/zen/go` base-variant quota endpoint is normalized and usage state survives account updates.
- Billing: per-channel reasoning-effort multipliers, final reasoning effort preserved across forwarding paths, and scientific notation at token boundaries parsed.
- Claude Code client version numbers are synchronized automatically.
- Simple mode can enable API key consumption window limits; first-start default group creation is now optional.
- Backups support monthly archive with an independent retention policy.
- Affiliate offline withdrawals are registered idempotently via Idempotency-Key.
- Rolling log retention is configurable; content audit gains independent TypeSafe engine configuration profiles.
- Official tool-schema cleaning strips illegal null `required` and `prefixItems`/tuple arrays; Antigravity resolves bare Gemini model names to thinking variants at every forwarding entry; streaming ends on the terminal event without waiting for upstream EOF.
- Plus closes Codex OAuth outbound divergences across custom-CA rotation on the HTTP and auth-plane client pools, WebSocket metadata header handling, credits-only rate-limit events, `include:["reasoning.encrypted_content"]` merges, transport-refusal handling, and rollout budget unit recording.

## Compatibility and migration

Migrations 238b, 240, and 269 add `content_moderation_logs.engine_meta`, the idempotent affiliate withdrawal `operation_id`, and per-usage `codex_rollout_budget_units`. Back up the database before upgrade. Rollback image is `v0.2.7+custom.001`.

## Known issues

None.

## Upstream baseline

Official release: v0.2.8
Official commit: fd80b08c90b55edcad5b00171b53f08721d30da1
