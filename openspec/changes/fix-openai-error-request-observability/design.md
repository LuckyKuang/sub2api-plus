## Scope and invariants

This change repairs the failures recorded by the production Error Requests
view.  It does not grant additional OAuth group access, alter retry budgets,
or weaken the Codex outbound-identity contract.  A valid credential-owning
account `credentials.user_agent` remains first priority, followed by a valid
global `openai_codex_user_agent`, followed by the compiled default.  Diagnostic
collection may report the selected source but must never influence selection.

## Responses item identifiers

Identifier validation is represented by an explicit item-type prefix matrix.
The existing `message → msg` and normal function/tool-call `→ fc` behavior is
retained, while `custom_tool_call → ctc` is added.  Mismatched identifiers are
removed before forwarding; matching identifiers and their `call_id` values are
preserved.  This prevents a valid `ctc_*` context item from being stripped and
then replayed with an invalid regenerated identifier.

## Retry-buffer and upstream transport failures

The exact `507` retry-buffer-limit signature is a local replay-safety
rejection, not an upstream account failure.  It is converted to a
request-scoped, non-retryable failover error with client status `413`; it does
not enter another-account failover, cooldown, disablement, or health path.

Connection resets/refusals before response headers and gateway timeout
responses are classified independently of model and credential failures.
Routing may still move to another eligible account where the existing bounded
policy allows it, but the failed account is not marked unhealthy solely from a
shared proxy/edge failure.  A proxy endpoint circuit is updated only from the
sanitized endpoint identity and transport classification, never from raw proxy
credentials.

## Selection and Ops diagnostics

The scheduler produces a typed, sanitized selection diagnosis when no account
can be selected.  It records candidate counts and mutually exclusive filtered
reasons (for example disabled, policy denied, temporarily unavailable, or
model/capacity constrained) rather than parsing its human-readable error
message.  Handlers propagate that diagnosis into the Ops error input.

Migration 219 adds `routing_diagnostics` JSON and
`is_routing_capacity_limited`.  The existing `is_business_limited` value is
preserved for SLA compatibility.  New routing failures set the new marker;
existing records in the routing phase are backfilled.  `view=errors` includes
routing-capacity failures, `view=excluded` contains only business-limited
records that are not routing-capacity failures, and `view=all` is unfiltered.

## Administration window and presentation

The date picker reports its selected preset to `UsageView`.  Only the
`last24Hours` preset emits `time_range=24h`; custom ranges continue to emit
explicit RFC3339 boundaries.  This prevents a two-calendar-day range from
being labelled as a rolling 24-hour query.  The error tab defaults to the
operational Error view and offers Excluded and All explicitly.  The error
detail panel renders only structured, sanitized diagnostics.

## Release

All release declarations, deployment examples, release notes, and UPSTREAM
mapping move from `v0.1.173+custom.001` to
`v0.1.173+custom.002`.  Publication remains a separate operation requiring
explicit authorization.
