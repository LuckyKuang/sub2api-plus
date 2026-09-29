# Upstream v0.2.9 integration

This source integration uses the official tag and commit recorded in
[UPSTREAM.md](../UPSTREAM.md). It layers `v0.2.9` onto the Plus tree that
already incorporated the official `v0.2.8` tag documented in
[v0.2.8 integration](UPSTREAM_V0_2_8_INTEGRATION.md). Importing the tag does
not publish a Plus release or change the embedded application version; the Plus
version stays `0.2.8+custom.002` and a later `0.2.9+custom.001` promotion is a
separate release-preparation change.

This increment ships no schema migrations. The official `v0.2.9` tree embedded
VERSION still reads `0.2.8`; the Plus VERSION, distribution source, and release
mapping are not overwritten by the tag. Back up the database before any upgrade.

## Official changes imported

- Antigravity retries an otherwise empty stream or a stream that failed with
  `MALFORMED_FUNCTION_CALL` only while no irreversible output has been
  committed; base64 PDF/document `inlineData` conversion and `const`/`enum`
  schema-intersection fixes are included.
- OpenAI/Anthropic bridges apply explicit `thinking` disabled, GPT-generation
  sampling-parameter filtering, role `message` typing, initial tool `input`
  retention, and terminal empty-text recovery without duplicating content.
- `OpenAI-Beta` support is added for Responses multi-agent and Anthropic
  structured-output paths under the Plus non-passthrough policy.
- WebSocket turns break the old `previous_response_id` continuation chain when
  the context-window ID changes; Responses probe keeps `model unavailable`
  unknown instead of persisting it as unsupported; Alpha Search fallback only
  reports a billable success on a genuine completed terminal.
- Client cancellation maps to HTTP 499 for uncommitted responses and continues
  the independent settlement, billing, and disconnect-risk lifecycle for
  already-accepted requests.

## Plus-preserved behavior (integration decisions)

| Area | Integrated behavior |
| --- | --- |
| Version / release | Plus VERSION `0.2.8+custom.002`, GHCR distribution, historical published mapping, and toolchain pins stay unchanged. The integrated upstream baseline moves to `v0.2.9`. |
| Identity / beta | Credential-owner identity precedence and terminal-send contract are unchanged. `OpenAI-Beta` is applied by the trusted resolver, not by inbound UA/Originator/Version selection; drop policy and signature coherence are preserved. |
| Grok | The fixed-point Grok OAuth/API Key routing, shell identity, managed headers, probe/refresh snapshot, and restrictive fallback are kept; generic changes do not rewrite managed headers. |
| Streaming / usage | Plus `responseCommitted`, real terminal detection, audio metering, partial-usage handling, and output timing remain authoritative; the official empty-stream retry feeds the existing failover without double-send or phantom first token. |
| Cancellation | Client disconnect is classified from the real request context; a lone upstream `context.Canceled` on a live client context is not treated as client disconnect. 499 marking does not bypass accepted-request settlement or punish the provider. |
| Billing | Plus per-reasoning-effort (audio/video/native Anthropic) multipliers, final model/effort, auto-review and Luna mismatch protection, account/group gating, and Fast/Flex rules remain. Channel image price left blank now inherits the catalog price; an explicit `0` on image output stays free and does not fall back to text pricing. Account-stats catalog-price branching uses account long-context gating and is not controlled by the group customer-price switch. Free Fast missing-price keeps the zero-cost usage log without swallowing other settlement errors. |
| Conversion limits | Imported `const`/`enum`, PDF/base64 `inlineData`, and reasoning-placeholder fixes keep target-model constraints. `file_id-only` and PDF text extraction are not claimed; the DeepSeek reasoning placeholder stays scoped to its target models and official OpenCode Zen/Go DeepSeek combinations. |
| Quota / scheduling | Confirmed no-card query suppression, failed-query backoff, usage-vs-card idempotency separation, future-reset-time pause, and the scheduling projection fields are imported; retired upstream billing-probe fields are not resurrected. |
| Allowlist / discovery | Model rules accept `*` anywhere (case-insensitive full-segment match); `?` and `[]` gain no wildcard meaning. Client model precedence, alias equivalence, empty-list validation, HTTP/WS admission, and pass-through vs. mapped discovery are unchanged, and supplemented discovery cannot bypass the group allowlist. Historical migration `262_normalize_legacy_model_allowlist.sql` is left intact. |
| Frontend / deploy | CC Switch keeps the Grok/Codex defaults and adds trailing-slash/dedup handling; the Windows Codex model catalog uses the official `~/` fix; model plaza shows an independent video multiplier consistent with billing; idle reset-time countdown and dialog cleanup are fixed. Redis `exec` list form applies to the affected Plus Compose variants without overwriting Plus security or repository configuration. The install wizard stops emitting the obsolete `rate_limit` default without altering valid existing configs. |
| Ingress audit | The two-engine ingress audit boundary, canonical extraction pass-through for unknown/unextractable content, and structured desensitized logging remain the immutable Plus contract. |

## Upgrade notes

- Channel image prices intentionally left blank now inherit their catalog price
  instead of being forced to zero; keep or set an explicit `0` to preserve a
  free configuration.
- Group model allowlist entries may place `*` at any position; the previous
  "trailing only" restriction is removed. Existing databases can store the new
  syntax at runtime; the historical one-time migration validation is unchanged.

## Validation boundary

All local generation and validation runs with the pinned repository toolchain
in Apple Containers on macOS, Docker inside WSL2 Debian/Ubuntu on Windows, or
Docker on Linux. Host-side validation is forbidden. Relevant backend suites,
frontend lint/typechecking and Vitest, and the policy checks are required for
this integration. PR submission additionally requires the official full local
matrix through the repository submission CLI. Local test success is not evidence
of a published release or a production upgrade.
