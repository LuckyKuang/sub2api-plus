# Upstream v0.2.8 integration

This source integration uses the official tag and commit recorded in
[UPSTREAM.md](../UPSTREAM.md). It layers `v0.2.8` onto the Plus tree that
already incorporated the official `v0.2.7` tag documented in
[v0.2.7 integration](UPSTREAM_V0_2_7_INTEGRATION.md). Importing the tag does
not publish a Plus release or change the embedded application version.

Migrations 240 and 269 ship in this import: the idempotent `operation_id` on
`user_affiliate_ledger` and `codex_rollout_budget_units` on `usage_logs`.
Back up the database before upgrade.

## Public API and scheduling

- New models: GPT-6 Sol, GPT-6 Luna, Claude Opus 5.5, and Grok 4.7.
- OpenCode Go usage window: official quota query, automatic refresh, same-key
  group sharing, and manual query. Account list and usage cells show 7d/1m
  balance badges; the `/zen/go` base-variant quota endpoint is normalized and
  managed usage state survives account updates.
- Billing supports per-channel reasoning-effort multipliers and keeps the final
  reasoning effort across forwarding paths. Usage logs record the requested
  `codex_rollout_budget_units` as a reserved billing dimension.
- Claude Code client version numbers are synchronized automatically.
- Simple mode can require API key consumption window limits; the optional
  first-start default group creation is now opt-in.
- Backups support monthly archives with an independent retention policy;
  inherited saves keep S3 secrets encrypted.
- Affiliate offline withdrawals are registered idempotently by
  `Idempotency-Key`; payment callback base URLs lose their trailing slash.
- Rolling log retention policy is configurable.
- Codex credits are displayed and referral invitations managed; plugin HostService account directories return structured read-only metadata.
- Tool-call schemas strip illegal null `required` entries and
  `prefixItems`/tuple arrays to avoid upstream 400s.
- Antigravity resolves bare Gemini model names to thinking variants at every
  forwarding entry, avoids Claude Agent SDK attribution in system prompts
  during capacity pressure, and lists mixed Antigravity models for Gemini
  groups.
- Scheduling routes by `previous_response` to the account holding the response
  for non-advanced scheduling, falls back to account multipliers, and keeps
  per-account RPM in the scheduling projection.
- Streaming ends on the terminal event without waiting for upstream EOF;
  OpenAI HTTP/2 keepalive tolerance is restored and response attempts are
  cancelled before closing the body.

## Plus integration decisions

| Area | Integrated behavior |
| --- | --- |
| README / sponsors | Keep Plus README structure, distribution links, and section IDs. Official sponsor-table churn is not imported. |
| Identity | Credential-owner identity precedence is unchanged. Plus closes the remaining Codex OAuth outbound divergences against the official `codex-rs` client: custom-CA rotation on the HTTP and auth-plane client pools, WebSocket in-band model headers, credits-only rate-limit events, the official `include:["reasoning.encrypted_content"]` merge, transport-refusal handling, and rollout budget units. |
| Ingress audit | Security-audit order and extraction pass-through remain the Plus contract. The upstream TypeSafe engine profile split is not imported: Plus keeps a single audit pipeline with its own keyword, session-block, and cyber-policy behavior, so the engine profile config surface and files stay out of this tree. |
| Usage / quota | Plus session affinity, local quota views, pause thresholds, and the five-level quota reads remain authoritative; OpenCode Go usage-window parity follows the official API. |
| Billing | Plus billing, quota, and scheduling hooks apply unchanged to imported model and multiplier pricing; the rollout budget units column stays a reserved dimension. |
| Backup / affiliate | Official monthly archive and offline withdrawal flows are included; Plus refund, balance, and concurrency fields remain. |
| Egress metadata | Plus proxy egress timezone/country annotations and the global egress country setting remain authoritative for the Codex visible environment. |
| Billing probes | Retired upstream billing probes remain removed. |

## Validation boundary

All local generation and validation runs with the pinned repository toolchain
in Apple Containers on macOS, Docker inside WSL2 Debian/Ubuntu on Windows, or
Docker on Linux. Host-side validation is forbidden. Relevant backend suites,
frontend lint/typechecking and Vitest, and the existing v0.2.7 upgrade
regression remain required for this integration. PR submission additionally
requires the official full local matrix through the repository submission CLI.
Local test success is not evidence of a published release or a production
upgrade.
