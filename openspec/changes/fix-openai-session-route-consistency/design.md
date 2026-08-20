## Routing invariants

In the default hard-affinity mode, the group-local sticky session is the
canonical route for a client session and account priority is consulted only
when no valid route exists. Explicit sticky-weighted mode remains score-based.
Account status, runtime exclusions, capability, transport, and current group
membership remain hard eligibility constraints in both modes. Removing an
account from a group must invalidate both ordinary session affinity and
response-chain affinity for that group.

`previous_response_id` remains a stronger continuation constraint when the
request cannot reconstruct its context. When the request is movable, an
already-established session route may supersede an older response binding; the
handler must then remove the response ID before forwarding. This makes a
failover selected by any endpoint visible to the other endpoints without
sending an account-owned response ID to a different account.

Sticky escape is a temporary exception to the hard-affinity route and keeps the
canonical session binding unchanged. For a movable continuation, an older
response binding must not pull the escaped request back to the degraded
account. Candidate ordering uses the stable session hash alone whenever it is
available; `previous_response_id` is only a fallback seed when no stable
session exists, because it is not shared by Alpha Search.

## Canonical Codex session signal

Session resolution keeps the current precedence: direct stable session headers
first, then the stable `session_id` string inside `X-Codex-Turn-Metadata`, then
body `prompt_cache_key`, content-derived identity, or an endpoint fallback.
`turn_id` is deliberately ignored because it changes per request. Malformed
metadata and non-string or empty session values are ignored.

The same direct-header-first resolution is used when persisting
`usage_logs.session_id` and deriving cyber-session block keys. Persisted values
pass through the existing length/control-character sanitizer; routing-only
fallbacks such as search IDs, request IDs, content hashes, and `turn_id` are
not written as client session IDs.

This resolution occurs on inbound client values before outbound OAuth
fingerprint convergence. It does not change the credential-owned/global/default
outbound identity precedence or reuse an upstream-rewritten identifier as a
client routing seed.

## Group invalidation and OAuth boundary

The response-account resolver must check the freshly loaded account with the
same `openAIAccountMatchesSchedulingGroup` predicate used by ordinary sticky
routing. On mismatch it deletes only the current group's local
response-to-account binding and returns a cache miss so normal scheduling can
choose an authorized account. It must not delete the sharing-enabled OAuth
owner or scope markers, because those markers prevent another user or obsolete
scope from reviving the response chain.

For sharing-enabled OAuth, selecting another account against an existing
response ID normally remains fail-closed. The only exception is a movable
request whose scheduling decision did not hit that response binding, because
the handler removes the ID before any upstream request. Non-movable tool
continuations and ownership mismatches continue to fail closed.

An already-established Responses WebSocket cannot change its credential-owning
upstream account safely between turns. Every ingress mode therefore invokes the
same `BeforeTurn` boundary before a `response.create` is sent upstream. The
adapter resolves the current client/channel model first, then the boundary
reloads the selected account directly from the durable repository and applies
the same persisted hard account gates as a fresh HTTP selection: current group
membership, status, schedulability, quota pause, client-model support, endpoint
capability, transport, shadow-parent health, runtime blocks, scheduling
thresholds, and proxy stream quarantine. Model eligibility uses the current
client model, matching HTTP account selection; the channel-mapped model remains
the upstream and billing mapping rather than replacing the account whitelist
identity. A mismatch or refresh failure closes the client connection with a
retryable status; the next connection performs normal scheduling.

The initial WebSocket turn retains the single billing check performed before
account selection. Every later turn reruns the same billing eligibility method
before account revalidation, so balance, subscription, user-platform quota,
API-key limits, and RPM apply once per accepted turn. The follow-up request
boundary also repeats image-generation group permission and derives the turn's
endpoint capability before `BeforeTurn`; this closes the passthrough gap while
ctx-pool and HTTP bridge retain their payload-level checks. All ingress modes
then share the same per-turn profit, pricing, and concurrency lifecycle.

## Response continuation safety

Client-facing Responses WebSocket ingress already analyzes tool-output
coverage. A request is movable when it has no tool output, or every tool-output
call ID has matching call context or an item reference. Only a movable request
that did not reuse the response binding may have `previous_response_id`
removed. HTTP Responses continues to reject `previous_response_id` because
that continuation mode requires Responses WebSocket v2.
