## 1. Request identity and Compact behavior

- [x] 1.1 Accept current Codex session/thread headers and preserve Compact
  cache fields through ingress normalization.
- [x] 1.2 Resolve one stable tenant-isolated cache identity and align the body
  key with the canonical upstream session header in HTTP, client-facing WS
  ingress/passthrough, and transformed Responses paths after fingerprint/header
  finalization; keep pooled WS handshakes isolated by that identity.
- [x] 1.3 Auto-derive API-key Chat-to-Responses cache identities while leaving
  raw Chat Completions forwarding unchanged.
- [x] 1.4 Apply account/model capability filtering to `prompt_cache_options`.

## 2. Usage and presentation

- [x] 2.1 Add presence-aware top-level cache read/write fallbacks with shared
  HTTP/WS priority, without overriding canonical nested values.
- [x] 2.2 Add a shared frontend prompt-cache hit-rate helper and display the
  rate from input-side buckets in dashboard and key-usage views.
- [x] 2.3 Keep English and Chinese locale keys and frontend API types aligned.

## 3. Verification

- [x] 3.1 Add Go coverage for Compact preservation, canonical headers,
  body/header alignment, stable fallback keys, API-key Chat conversion,
  model-aware cache options, fingerprint finalization, seed-only retry
  idempotence, WS identity inheritance/handshake compatibility, retention
  removal, and usage alias precedence.
- [x] 3.2 Add Vitest coverage for 0%, 25%, 100%, zero-denominator, and clamped
  cache-hit-rate presentation.
- [x] 3.3 Run focused backend tests, frontend lint/typecheck/Vitest, strict
  OpenSpec validation, formatting checks, and `git diff --check`.
