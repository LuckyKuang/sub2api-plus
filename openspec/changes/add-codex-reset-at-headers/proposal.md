## Why

Current Codex clients consume absolute `X-Codex-*-Reset-At` response headers.
The gateway currently exposes and parses only legacy relative
`Reset-After-Seconds` values, so clients can receive usable percentages and
window lengths without the authoritative reset timestamp.

## What Changes

- Dual-write `X-Codex-Primary-Reset-At` and `X-Codex-Secondary-Reset-At` with
  the existing relative reset headers for local quota responses.
- Preserve the absolute headers during eligible upstream HTTP/SSE passthrough.
- Parse absolute upstream reset headers into the existing relative snapshot
  representation, preferring valid absolute values and retaining legacy
  fallback behavior.
- Document upstream passthrough for HTTP/SSE and the pre-upgrade local-header
  boundary for client-facing WebSocket `101` responses.

## Non-goals

- Changing Codex App API-key support for `account/rateLimits/read`.
- Adding persistent schema fields, database migrations, configuration flags,
  frontend changes, or removing legacy headers.

## Impact

- Affected capability: `codex-rate-limit-headers`.
- Affected code: OpenAI passthrough response writing, local subscription quota
  overlay, upstream Codex limit parsing, and 429 reset scheduling.
- Compatibility: additive client-facing headers with retained legacy behavior.
