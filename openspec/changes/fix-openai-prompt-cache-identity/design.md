## Scope and invariants

The change covers OpenAI Responses requests, legacy Responses Compact, and
Chat Completions requests whose selected API-key or OAuth account actually
uses the Responses upstream.  Raw `/v1/chat/completions` forwarding must not
receive Responses-only fields.

The existing Codex outbound identity precedence remains immutable: a valid
credential-owning account identity wins over the valid global identity, which
wins over the compiled default.  Cache/session normalization must not select a
different User-Agent, Originator, Version, OS, architecture, or terminal
fingerprint.

## Cache identity resolution

The stable seed order is: explicit body `prompt_cache_key`, canonical Codex
`session-id`, legacy supported session headers, and finally an anchored
content seed derived from model, instructions/system/developer content, tools,
and the first user input.  A content fallback requires a meaningful user/input
anchor so a model-only request cannot create one tenant-wide route.

The selected seed is resolved through the existing upstream-session isolation
policy.  Ordinary traffic is isolated by authenticated API-key ID.  OAuth
session sharing continues to use its account-scoped, authorized-group policy.
The resolved value is formatted as a deterministic UUID, is safely below the
64-character API limit, and is written to both `prompt_cache_key` and the
canonical `session-id` header.  This avoids the current raw-body-key versus
isolated-header mismatch.

The finalized body key is the cache-session source of truth after account
header overrides and OAuth fingerprint convergence.  The request builder
reapplies it to both `session-id` and the supported `session_id` alias after
those stages.  Fingerprint-owned installation, thread, window, turn metadata,
and outbound User-Agent identity remain unchanged; `thread-id` is then paired
again with `x-client-request-id`.  Without a body key, the existing fingerprint
session behavior remains intact.

The request-scoped idempotence marker stores the original seed and the
resolved session-sharing scope as well as the finalized identity.  Repeated
finalization in the same scope is a no-op.  If failover retries an already
finalized body in a different OAuth sharing scope, resolution restarts from
the original seed instead of treating the previous scope's UUID as a new
client seed.

Client-facing Responses WebSocket ingress finalizes every `response.create`
before relay.  Follow-up frames that omit `prompt_cache_key` inherit the
identity established by the first frame.  A changed explicit identity requires
a new upstream handshake and therefore a healthy pool-mode connection is
released and reacquired; connection reuse compatibility includes the finalized
session identity.  A `store=false` continuation that depends on connection-local
`previous_response_id` state, and passthrough mode where the upstream connection
cannot be replaced transparently, reject an in-connection identity change
instead of forwarding a body/header mismatch.

Official `thread-id` and `x-client-request-id` are request/thread identifiers,
not cache keys.  They are forwarded through the bounded Codex header
allow-list, and when both are supplied their official relationship is
preserved.  They never replace the session-scoped cache identity.

## Compact and cache options

The Compact ingress normalizer keeps the union needed by public Platform
Compact and the current Codex internal Compact payload.  It retains
`prompt_cache_key` and `prompt_cache_options`, while request-scoped `store` and
`stream` remain removed.

Account/model-aware finalization happens only after account selection.
ChatGPT OAuth removes `prompt_cache_options`.  OpenAI Platform API-key traffic
keeps it only for a GPT-5.6 family model; older or unknown model families have
it removed to avoid an upstream unsupported-parameter failure.  Deprecated
`prompt_cache_retention` is not restored.

## Usage parsing and hit-rate semantics

For cache read and write counts, canonical nested fields have presence-based
priority, including an explicit zero.  Top-level aliases are consulted only
when no canonical nested field exists.  Top-level fallbacks also use field
presence and one shared HTTP/WS priority order, so an explicit zero in the
highest-priority alias cannot be replaced by a positive legacy alias.  This
prevents nonstandard duplicate fields from inflating recorded usage.

OpenAI reports total input with cache details included.  The billing path
already converts this into mutually exclusive persisted buckets:

`ordinary_input = max(total_input - cache_read - cache_write, 0)`

Therefore the displayed hit rate is:

`cache_hit_rate = cache_read / (ordinary_input + cache_read + cache_write)`

The rate is zero/empty when the input-side denominator is zero and is clamped
to the inclusive 0–100 percent range for defensive presentation.  Output and
media-output tokens are never part of the denominator.
