## 1. Routing behavior

- [x] 1.1 Read the stable Codex turn-metadata session ID in the common OpenAI
  session resolver and keep direct-header precedence.
- [x] 1.2 Revalidate response-account bindings against current persisted group
  membership and clean stale local bindings for every OpenAI account type.
- [x] 1.3 Prefer a migrated session route for movable WebSocket continuations
  and preserve the existing response-ID removal safety check.
- [x] 1.4 Permit authorized sharing-enabled OAuth migration only when the old
  response ID is guaranteed to be removed.
- [x] 1.5 Keep movable sticky escape from falling back to the old response
  owner, and make stable-session candidate ordering endpoint-independent.
- [x] 1.6 Revalidate complete durable account eligibility before every turn of
  an existing Responses WebSocket and invoke the boundary in passthrough mode
  as well as ctx-pool and HTTP bridge modes.
- [x] 1.7 Recheck billing on each follow-up turn, retain exactly one first-turn
  billing check, and enforce follow-up image permission/capability before
  upstream writes.
- [x] 1.8 Persist the sanitized turn-metadata session in usage correlation and
  use it consistently for cyber-session blocking.

## 2. Documentation and verification

- [x] 2.1 Document the turn-metadata routing signal and hard/soft invalidation
  behavior in the OpenAI Responses protocol guide.
- [x] 2.2 Add focused tests for direct-header precedence, cross-endpoint session
  hashes, API-key and ordinary/shared OAuth group removal, migrated session
  precedence, movable sticky escape, endpoint-independent selection seeds, and
  non-movable fail-closed behavior, including passthrough hook ordering,
  full-account and billing rejection before an ineligible turn reaches
  upstream.
- [x] 2.3 Run Go formatting, focused backend tests, strict OpenSpec validation,
  and `git diff --check`.
