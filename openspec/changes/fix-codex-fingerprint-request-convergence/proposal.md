## Why

Codex fingerprint convergence currently treats native remote compaction and
the ChatGPT Codex OAuth legacy compact compatibility protocol as one exclusion.
Native compaction therefore ignores an OAuth credential owner's explicit mode,
while the legacy
protocol also loses the stable installation identity that does not belong to
its compact cache namespace. Several Responses bridges and WebSocket paths do
not prepare or apply the same request-scoped fingerprint state, and malformed
embedded turn metadata can retain conflicting identity or panic on JSON null.

## What Changes

- Classify ordinary Responses, native compaction, legacy compaction, and
  non-session requests explicitly.
- Apply the credential owner's complete mode to ordinary and native Responses;
  apply installation-only convergence to non-off legacy compact requests.
- Share one preparation path across HTTP, passthrough, Chat/Messages bridges,
  HTTP-to-WebSocket, and direct Responses WebSocket turns.
- Keep Plus prompt-cache identity authoritative for final session aliases.
- Rebuild existing malformed, null, or non-object embedded metadata safely.
- Remove the unused parallel Codex outbound identity resolver.

## Compatibility

The persisted `off`, `device`, `session`, and `full` values and the `device`
default remain unchanged. Stable IDs continue using the existing account/device
derivation; no seed or migration is introduced. Native compaction begins
honoring the configured mode. Legacy compact keeps its session, cache, and
thread namespace while gaining the credential owner's installation identity
when the mode is not `off`.

`Legacy compact` in this change is scoped to the ChatGPT Codex OAuth
compatibility branch. It does not claim that the public API-key
`/v1/responses/compact` endpoint is unavailable.

## Impact

- Affected capabilities: `codex-fingerprint-convergence`,
  `openai-codex-identity`.
- Affected code: OpenAI Responses HTTP/passthrough builders, compatibility
  bridges, WebSocket ingress/upstream builders, fingerprint helpers, and tests.
- Affected documentation: OpenAI Responses and Codex client profiles.
- Data and migrations: none.
