# Upstream v0.2.5 integration

This source integration uses the official tag and commit recorded in
[UPSTREAM.md](../UPSTREAM.md). It layers `v0.2.5` onto the Plus tree that
already incorporated the pinned official `main` snapshot documented in
[v0.2.4 integration](UPSTREAM_V0_2_4_INTEGRATION.md). Importing the tag does
not publish a Plus release or change the embedded application version.

No new SQL migrations ship in this import.

## Public API and scheduling

- DeepSeek accounts without `model_mapping` admit only the official model
  names. Unknown names return `404 model_not_found` at scheduling. See
  [DeepSeek](providers/DEEPSEEK.md).
- Gateway-synthesized Responses SSE, compact bridges, and WebSocket-to-HTTP
  error frames always emit `sequence_number`, including `0`. See
  [Responses stream sequence numbers](protocols/OPENAI_RESPONSES.md#responses-stream-sequence-numbers).
- OpenAI scheduler quota scoring reads canonical `codex_5h` / `codex_7d`
  windows first and pairs usage with the same window's reset time. See
  [Codex scheduler quota windows](protocols/OPENAI_RESPONSES.md#codex-scheduler-quota-windows).
- Antigravity Gemini SSE passthrough no longer inserts a blank line between
  already-terminated events. See [Antigravity](providers/ANTIGRAVITY.md).
- The user API-key create form can filter available groups by provider
  family. Classification uses the group's configured platform, not its
  display name.

## Plus integration decisions

| Area | Integrated behavior |
| --- | --- |
| README / sponsors | Keep Plus README structure, distribution links, and section IDs. Official sponsor-table churn is not imported. |
| Identity | Credential-owner identity precedence is unchanged. This import does not alter User-Agent, Originator, or Version selection. |
| Ingress audit | Security-audit order and extraction pass-through remain the Plus contract. |
| DeepSeek | Official empty-mapping whitelist is included. Passthrough accounts still skip mapping admission. |
| Quota / session | Canonical 5h/7d window reads are included. Plus session affinity, local quota views, and pause thresholds remain authoritative. |
| Capacity shed | OpenAI overloaded / `slow_down`, Anthropic `overloaded_error` / 529, Grok shared model capacity, and Antigravity `MODEL_CAPACITY_EXHAUSTED` return immediately. Same-account retry and account failover stay for transport, 401/403, and account-scoped 429. |
| API keys | Provider filter is included. Plus IP restriction, quota, rate-limit, and expiration fields on the same form remain. |
| Billing probes | Retired upstream billing probes remain removed. |

## Validation boundary

All local generation and validation runs with the pinned repository toolchain
in Apple Containers on macOS, Docker inside WSL2 Debian/Ubuntu on Windows, or
Docker on Linux. Host-side validation is forbidden. Relevant backend suites,
frontend lint/typechecking and Vitest, and the existing v0.2.4 upgrade
regression remain required for this integration. PR submission additionally
requires the official full local matrix through the repository submission CLI.
Local test success is not evidence of a published release or a production
upgrade.