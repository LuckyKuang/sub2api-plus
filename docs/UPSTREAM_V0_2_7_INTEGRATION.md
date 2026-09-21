# Upstream v0.2.7 integration

This source integration uses the official tag and commit recorded in
[UPSTREAM.md](../UPSTREAM.md). It layers `v0.2.7` onto the Plus tree that
already incorporated the official `v0.2.5` tag documented in
[v0.2.5 integration](UPSTREAM_V0_2_5_INTEGRATION.md). Importing the tag does
not publish a Plus release or change the embedded application version.

No new SQL migrations ship in this import.

## Public API and scheduling

- Seedance video tasks are served natively at `POST
  /api/v3/contents/generations/tasks` alongside the existing gateway routes.
  See [Seedance](seedance-api.md).
- User redemption history keeps its legacy array response when no pagination
  query is present; `?page=` / `?page_size=` switch to the paginated envelope
  with per-user isolation and stable ordering (`page_size` capped at 100).
- Plugins gain a generic `HostService` dialed over the go-plugin broker id
  delivered by the optional `InitHostServices` hook: namespaced KV storage,
  capability-scoped account enumeration, and outbound-identity resolution.
  Older plugins that leave `InitHostServices` unimplemented keep working.
  See [plugin API](../backend/pkg/pluginapi/README.md).
- Plugin `Health` and `TestConfig` responses carry an optional `status_json`
  blob that the config UI can surface; the host ignores it for health gating.
- DeepSeek chat fallback forwards thinking-mode `reasoning_content`; Responses
  tool-output images are lifted and parallel tool outputs stay contiguous. See
  [DeepSeek](providers/DEEPSEEK.md).
- Antigravity Gemini native requests resolve bare model names to
  `-low`/`-medium`/`-high` thinking variants and stop sending SSE comment
  heartbeats to go-genai/python-genai clients. See
  [Antigravity](providers/ANTIGRAVITY.md).
- CN coding-plan accounts pause on quota-exhausted `403`.
- Group usage rollups no longer full-scan `usage_logs`.

## Plus integration decisions

| Area | Integrated behavior |
| --- | --- |
| README / sponsors | Keep Plus README structure, distribution links, and section IDs. Official sponsor-table churn is not imported. |
| Identity | Credential-owner identity precedence is unchanged. This import does not alter User-Agent, Originator, or Version selection. The plugin `ResolveOutboundIdentity` host service resolves the same-account snapshot used by HTTP/WS paths. |
| Ingress audit | Security-audit order and extraction pass-through remain the Plus contract. |
| Redeem history | Official pagination is included. Plus refund, balance, and concurrency fields on the same handler remain. |
| Plugin host services | Generic KV store, account directory, and outbound-identity resolution are included; capability scoping and Plus plugin manager routing remain authoritative. |
| Seedance | Native Ark video task API is included; Plus billing, quota, and scheduling hooks apply unchanged. |
| Egress metadata | Plus proxy egress timezone/country annotations and the global egress country setting remain authoritative for the Codex visible environment. |
| Quota / session | CN coding-plan quota pause is included. Plus session affinity, local quota views, and pause thresholds remain authoritative. |
| Billing probes | Retired upstream billing probes remain removed. |

## Validation boundary

All local generation and validation runs with the pinned repository toolchain
in Apple Containers on macOS, Docker inside WSL2 Debian/Ubuntu on Windows, or
Docker on Linux. Host-side validation is forbidden. Relevant backend suites,
frontend lint/typechecking and Vitest, and the existing v0.2.5 upgrade
regression remain required for this integration. PR submission additionally
requires the official full local matrix through the repository submission CLI.
Local test success is not evidence of a published release or a production
upgrade.
