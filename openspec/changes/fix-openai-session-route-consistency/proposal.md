## Why

OpenAI account-group edits and Codex multi-endpoint conversations currently use
two partially independent affinity paths. A stored `previous_response_id`
binding is revalidated for account health and OAuth sharing policy, but not for
ordinary persisted group membership. Removing an API-key or non-sharing OAuth
account from a group can therefore leave that group routed to the removed
account until the response binding expires.

Codex `/v1/responses` and `/v1/alpha/search` requests can also carry the same
stable session inside `X-Codex-Turn-Metadata`, but the gateway only reads direct
session headers. Alpha Search then falls back to its per-search ID and can move
to a newly higher-priority account while Responses remains on the established
session account. A later safe failover can be blocked again by the old response
chain, particularly for OAuth accounts with session sharing enabled.

## What Changes

- Treat `X-Codex-Turn-Metadata.session_id` as a stable session signal after
  direct session headers and before endpoint-specific fallbacks.
- Revalidate every `previous_response_id` account against the current persisted
  scheduling group, including API-key, ordinary OAuth, and sharing-enabled
  OAuth accounts; remove only the stale group-local binding on mismatch.
- Revalidate the complete persisted hard eligibility of an already-selected
  account before every turn on all Responses WebSocket ingress modes,
  including group membership, status, schedulability, quota pause, current
  client model, endpoint capability, transport, parent health, runtime blocks,
  scheduling thresholds, and proxy quarantine.
- Recheck billing eligibility before every follow-up WebSocket turn and apply
  follow-up image permissions/capability routing before upstream writes, so a
  long-lived connection cannot bypass checks performed by a fresh HTTP
  request.
- Make an already-migrated session binding authoritative over an older response
  binding when the request contains enough context to move safely.
- Use the existing WebSocket tool-continuation coverage decision to remove
  `previous_response_id` only when routing did not reuse its account and the
  request can reconstruct the continuation from its input.
- Allow sharing-enabled OAuth failover to select an authorized replacement
  account when the stale response ID will be removed, while preserving the
  global owner marker and fail-closed behavior for non-movable or foreign
  continuations.

## Impact

- Account group removal becomes a hard routing invalidation as soon as the
  persisted account is rechecked, including on the next turn of an existing
  WebSocket connection; under the default hard-affinity mode, priority changes
  remain soft and do not move an active session. Explicit sticky-weighted mode
  retains its score-based behavior.
- Responses and Alpha Search share one account route when Codex supplies the
  same direct session ID or turn-metadata session ID.
- Usage records and cyber-session blocking use the same sanitized stable
  turn-metadata session when direct session headers are absent.
- Every accepted WebSocket turn observes current billing and account policy;
  an ineligible connection closes before the turn reaches upstream and a
  reconnect can select another account.
- A retryable account failure can migrate the shared route once; subsequent
  movable Responses WebSocket turns follow the replacement account instead of
  reviving the old response-account binding.
- No database migration, public request schema change, credential change, or
  outbound Codex identity precedence change is required.
