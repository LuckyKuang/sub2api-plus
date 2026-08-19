## Why

OpenAI Codex now sends a stable session identity in both the Responses body and
the canonical `session-id` header.  The gateway currently drops
`prompt_cache_key` from `/v1/responses/compact`, ignores the canonical Codex
session header for routing, and only auto-derives a key for OAuth
Chat-Completions-to-Responses traffic.  As a result, otherwise identical
prefixes can be routed under missing or mismatched cache identities for both
OAuth and API-key accounts.  OAuth fingerprint convergence can also overwrite
the already-aligned session header late in request construction, making the
body key and final wire identity diverge again.

Usage ingestion already stores ordinary input, cache-read input, and
cache-write input as mutually exclusive buckets, but several UI percentages
divide cache tokens by input plus output.  That value is a total-token share,
not the prompt-cache hit rate shown by Codex-compatible clients.

## What Changes

- Preserve `prompt_cache_key` on Compact requests and derive a stable key when
  a Responses request has no explicit cache identity.
- Recognize the current Codex `session-id`, `thread-id`, and
  `x-client-request-id` headers while retaining legacy header compatibility.
- Namespace the upstream cache identity by the authenticated API key (or the
  configured OAuth sharing scope), then keep the forwarded body key and
  canonical upstream session header identical.
- Auto-derive and forward a cache key for API-key Chat Completions requests
  that are converted to Responses, not only for OAuth requests.
- Preserve valid `prompt_cache_options` only for GPT-5.6-family OpenAI
  Platform Responses/Compact requests; remove it from ChatGPT OAuth and older
  model requests.  Deprecated `prompt_cache_retention` remains unsupported.
- Parse standard nested cache usage first and support known top-level
  cache-read/cache-write aliases only as presence-aware fallbacks.
- Expose cache hit rate in usage views using
  `cache_read / (input + cache_read + cache_write)`, excluding output tokens.

## Impact

- Public request behavior: Responses and Compact requests may gain a stable,
  opaque `prompt_cache_key`; explicit keys are tenant-namespaced before they
  reach the upstream.
- Protocol compatibility: current Codex hyphenated session/thread headers are
  accepted in addition to existing legacy aliases.
- Usage API storage remains unchanged; cache hit rate is derived from the
  existing mutually exclusive token buckets.
- Frontend usage and dashboard views gain a consistent cache-hit-rate display.
- No database migration, credential change, or outbound User-Agent identity
  precedence change is required.
