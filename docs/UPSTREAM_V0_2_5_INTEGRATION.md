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
- An upstream `401` on an OpenAI OAuth credential owner force-refreshes the
  credential and retries the same account once before failover; the attempt is
  recorded as an `auth_refresh_401` retry event. See the capacity-shed row
  below.
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
| Capacity shed | OpenAI overloaded / `slow_down`, Anthropic `overloaded_error` / 529, Grok shared model capacity, and Antigravity `MODEL_CAPACITY_EXHAUSTED` return immediately. Same-account retry and account failover stay for transport, 401/403, and account-scoped 429. An upstream 401 on an OpenAI OAuth credential owner first force-refreshes the credential and retries the same account once (recorded as an `auth_refresh_401` retry event); only a still-failing retry fails over. 403 keeps the direct failover behavior. A 429 declaring an exhausted account quota or credit (`insufficient_quota`, `credit_balance_exhausted`, `*_spend_limit_exceeded`, `usage_not_included`) is terminal: no same-account retry window opens, and `x-codex-active-limit` selects which metered family drives the reset decision instead of the default `codex` family. |

| WebSocket transport | A WebSocket turn that hits an upgrade refusal (426) or a "WebSocket unsupported" signal continues on the HTTP forwarding path and marks the account as falling back for the cooldown, matching the official `FallbackToHttp`; other WebSocket failures still surface the original error. The WebSocket read/idle timeout defaults to 300s, matching the official unified `stream_idle_timeout`. WebSocket retries replay the request payload unchanged, so the `include: ["reasoning.encrypted_content"]` declaration survives every attempt. |
| Passthrough normalization | A Codex-protocol passthrough body is normalized with the same unsupported-field set as the non-passthrough path, so `max_output_tokens`, `max_completion_tokens`, `temperature`, `top_p`, `frequency_penalty`, and `presence_penalty` are stripped before the upstream request instead of causing a first-request 400. Non-compact passthrough and WebSocket compatibility bodies also receive the official `include: ["reasoning.encrypted_content"]` merge. |
| WebSocket quota snapshot | An in-band `codex.rate_limits` event refreshes the account Codex usage snapshot the same way a response header does, so a WebSocket turn keeps the quota view current between WHAM pulls. Windows without a parseable `used_percent` are dropped, the credits family still requires both flags, and credits-only events (no `rate_limits` object) still refresh the credits snapshot, matching the official `parse_rate_limit_event`. Limit name, credits, and extra families persist on account Extra as scheduler-neutral diagnostics. |
| Stream model observation | The server-declared model follows the official `response_model()` order: nested `response.headers`, then top-level `headers` on WebSocket metadata events. `response.model` remains a Plus billing fallback when neither header is present. |
| Environment context | The model-visible `<environment_context>` timezone/date alignment defaults to off: with no effective timezone configured at the account, proxy, or global level, the client's own pair reaches the upstream unchanged, as the official client renders it. |
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