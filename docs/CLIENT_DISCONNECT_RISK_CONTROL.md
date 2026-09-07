# Client Disconnect Risk Control

The client-disconnect risk control records paid upstream requests that end after
client cancellation and can optionally enforce a per-session consecutive limit
while preserving normal cancellation and billing behavior.
It is independent of Content Moderation and Prompt Audit policy decisions.

## Settings

Administrators configure the feature under Settings > Risk Control.

| Setting | Default | Valid values | Meaning |
| --- | --- | --- | --- |
| `client_disconnect_consecutive_ban_enabled` | `false` | `true`, `false` | Enables automatic disabling for consecutive client disconnects. |
| `client_disconnect_consecutive_ban_threshold` | `10` | `1..1000` | Number of consecutive qualifying disconnects that disables an ordinary user. |

Changing the enabled switch advances an internal generation. Disabling and then
re-enabling the feature therefore starts a new streak. A threshold change
applies to requests finalized after the change and does not scan historical
users.

## Counting Contract

A request affects the streak only after an authenticated external request has
been accepted by an upstream service. Validation failures, account-selection
failures, internal probes, retries before upstream acceptance, and scheduled
tests do not count. OpenAI Responses WebSocket turns use the same rule at the
write boundary: they are counted after `BeforeTurn` succeeds (account
revalidation, billing eligibility, and concurrency), immediately before the
upstream write. A `response.create` that fails admission never creates an
event. If `BeforeTurn` succeeds but the first upstream write or handshake
fails, the turn is still finalized as a neutral/error outcome so the
ordered queue cannot stall on a pending event.

Settings changes invalidate the local process cache immediately. In a
multi-instance deployment, another process may observe the new value after the
short settings cache interval (currently at most two seconds). Once an enabled
switch update commits, repository-level persisted generation and enforcement
checks prevent stale instances from creating or enforcing events from the old
generation without waiting for that cache interval. Threshold-only changes
still follow the short settings cache interval.

Outcomes are applied in upstream-acceptance order for each user session:

| Outcome | Streak effect |
| --- | --- |
| Completed valid request | Reset to `0` |
| Client disconnected before valid completion | Increment by `1` |
| Upstream error, timeout, or other neutral result | No change |

This is a strict success reset within one session. For example, nine disconnects,
one successful request, and nine more disconnects in the same session leave that
session's streak at nine. A success in another session does not reset it. The next
qualifying disconnect reaches the configured threshold and disables the user when
automatic enforcement is enabled.

The server-generated client request ID remains the per-request idempotency key
within a generation. Client-provided correlation headers are never used for
request deduplication, so reusing `X-Request-ID` cannot suppress an event. The
repository serializes duplicate trusted request IDs across session scopes, so
an inconsistent retry cannot create a second event or surface a uniqueness
error. The persisted settings generation is checked before creating any new
session state, preventing an instance with stale settings from reopening an
older enforcement generation. Finalization also checks the persisted enabled
state and generation before applying any queued outcome, so disabling the
feature cannot allow an older cached lifecycle to enforce a ban. The sanitized
inbound session identifier independently selects the streak scope.
Current Codex clients use `session-id`; supported legacy and protocol-specific
aliases follow the canonical session extraction contract used by usage logs.
Concurrent requests receive per-session sequence numbers and are processed in
acceptance order rather than callback completion order. A pending request blocks
only later results in the same session. State generations only move forward; an
instance with a stale settings cache cannot roll a session back to an older
generation or delete current-generation events.

Requests without a valid explicit session identifier use a deterministic
API-key-scoped sessionless bucket. This preserves enforcement coverage without
mixing different API keys. Session identifiers are client-controlled correlation
data, not proof of a trusted device: a client that deliberately rotates session
IDs can avoid building one session streak. Operators who need adversarial,
user-wide abuse detection should use a separate rolling-window policy rather than
treating this consecutive-session control as device attestation.
OpenAI Responses WebSocket requests are counted per admitted turn; closing a
connection after a completed turn is not a disconnect outcome.

Automatic enforcement is disabled by default. Administrator and feature-disabled requests remain auditable, but their events
have effective `enforce=false` and do not increment or reset the streak. The
repository derives the administrator exemption again from the current database
role, so a stale caller cannot opt an administrator into enforcement. Promoting
a user to administrator or re-enabling a disabled user clears the current
streak. Automatic enforcement changes only an active ordinary user's status to
`disabled`; it does not delete the user, API keys, usage records, or billing
records.

## Billing And Settlement

Disconnect risk control does not make a canceled request free and does not
replace usage settlement. Streaming paths continue draining an accepted
upstream response where supported so final or partial usage can still be billed.
Non-stream forwarding also uses an independent settlement context after
upstream acceptance, bounded by a 15-minute total timeout; existing account
concurrency and response-body limits remain in force. If the client disconnects
while a non-stream request is settling, the terminal is classified as
`client_disconnected`; exact upstream usage remains billable and is recorded as
`usage_source=upstream_exact` rather than resetting the streak as a success.
Remote `count_tokens` and `responses/input_tokens` requests use the same bounded
settlement rule even though they are not billed. A valid remote count resets the
streak, a client disconnect after upstream acceptance increments it, and an
unsupported endpoint or invalid upstream response remains neutral. Count-token
requests resolved entirely by a local estimator never create a risk event.
Risk-control repository or settings failures are fail-open for request
forwarding: they are logged but do not interrupt upstream forwarding, usage
recording, or billing. Lifecycle begin/finalization operations retain enough
state to retry transient repository failures without changing the original
terminal outcome.

If the upstream never returns a result before the settlement timeout, the risk
event records the known lifecycle without inventing token usage or a charge.
Operators should treat repeated accepted requests with missing usage as a
reconciliation signal.

For ordinary billable usage, the billing deduplication claim, balance or quota
updates, and `usage_logs` insertion commit in one PostgreSQL transaction. Batch
image capture uses the same guarantee. A usage-log insert failure rolls back the
charge or capture claim; legacy deduplicated charges can create a missing log on
a later retry without charging twice.

## Operations

Automatic bans emit the structured event
`client_disconnect_risk.user_auto_banned`. Persistent details are stored in
`client_disconnect_risk_events`; current streaks are stored in
`client_disconnect_risk_states`.

Useful fields include `user_email`, `api_key_name`, `request_id`, `session_id`,
`protocol`, `outcome`,
`completion_status`, `usage_source`, `usage_missing`, `consecutive_after`,
`threshold`, `enforce`, `auto_banned`, `accepted_at`, and `finalized_at`. The
admin endpoint `GET /api/v1/admin/usage/client-disconnect-events` provides
fixed-order pagination and validated identity, correlation, protocol, time,
outcome, usage, and enforcement filters for these records. These records
contain request metadata, not raw prompts, credentials, errors, or response
bodies. The administrator Usage Records page exposes the same metadata in its
Client Disconnects tab, including missing-usage and auto-ban filters.

Lifecycle events are retained across settings generations so accepted-request
audit history is not silently lost. Storage cleanup must be an explicit,
operator-visible data-retention operation. When a process crash leaves an
accepted event pending, a later settlement may classify it as neutral after 24
hours so it cannot block the ordered queue forever. Neutral expiration never
increments or resets the streak.

After reviewing false positives, an administrator may restore the user through
the normal user-management flow. The database trigger clears the old streak as
part of the transition back to `active`. Do not manually edit risk-state rows.
Usage-worker `drop`/`sample` remains a queue scheduling option, but rejected
billing tasks now execute synchronously with a bounded context instead of being
silently lost; sustained fallback is still a capacity-planning and latency
alert.

## Current Coverage

The lifecycle is connected to Anthropic-compatible Messages, Responses and Chat
Completions; Gemini v1beta; OpenAI Responses, Messages, Chat Completions,
Images, Embeddings and Alpha Search; Grok media and voice HTTP; standalone Grok
Web Search and X Search; remote Anthropic/OpenAI token-count endpoints; and
OpenAI Responses WebSocket turns. Grok Voice Realtime is counted per upstream
response turn, not per connection: `response.created` accepts a turn, successful
terminal events reset the streak, failed terminal events are neutral, and only
still-pending response IDs become disconnected when the connection closes. A
missing user role skips Grok Realtime counting so the administrator exemption
cannot be lost.
