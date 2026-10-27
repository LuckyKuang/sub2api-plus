# Gateway request admission

Authenticated gateway endpoint families share the user's execution-concurrency
and request-rate policies across API keys, groups, providers, and request
origins. Main-agent and sub-agent requests use the same user identity. A client
header, model alias, account retry, or provider change does not create another
user or grant capacity. See [global concurrency](COMPOSITE_GROUPS.md#user-concurrency-across-suppliers-and-agent-requests)
and [unfinished asynchronous tasks](GATEWAY_TASK_LIMITS.md) for the separate
execution and durable-task limits.

A full user pool rejects external requests immediately without a user wait
queue. Account scheduling and the private durable-worker handoff may wait only
within their bounded queue policy. A successful capacity reply arriving after
that queue or request deadline is returned to its exact owner without starting
upstream execution; queue accounting is released as well. A slot accepted before
the deadline keeps its execution lease independently of the completed wait
scope, and lost capacity cancels its upstream work.

## User and user-group RPM

Two request-rate limits apply in parallel:

| Policy | Counter scope | Meaning |
| --- | --- | --- |
| `user.rpm_limit` | User across all keys and groups | Global ceiling; `0` is unlimited. A group policy never replaces it. |
| `group.rpm_limit` | One `(user, group)` pair | Default ceiling for each user of the group; `0` is unlimited. It is not a shared counter for every user in that group. |
| `(user, group).rpm_override` | The same `(user, group)` pair | Replaces only that group's default for this user. Absent inherits the default; `0` exempts this pair; a positive value sets its ceiling. |

Any applicable exceeded limit rejects admission. For example, a user ceiling
of `3` still permits at most three admitted requests in the minute when its
group default or override is `100`. An override of `0` skips only the group's
check; the user's ceiling continues to apply. Distinct API keys for the same
user share these counters. There is no independent API-key RPM policy.

The counter window is a fixed natural minute from Redis server time
(`Unix seconds / 60`), shared across gateway instances. It is not a rolling
60-second window. `INCR` and the 120-second cleanup expiry run in a Redis
transaction; counts exceeding the configured ceiling are rejected. The expiry
does not lengthen the rate window. Requests around a minute boundary can use
the allowances of both minutes.

The gateway checks the group policy before the global user policy. A group
rejection ends admission without another user-counter increment. Unlimited
policies do not require their corresponding increment. Requests rejected by
authentication, basic validation, security audit, or user-concurrency admission
do not reach this RPM gate.

## One count per external request

All authenticated gateway endpoint families use the same admission policy,
including inference, model discovery, usage metadata, token counting, native
media, Files, resource controls, and asynchronous task status/content/delete.
Configuration and management APIs, including the available-channel and sale
catalogs, are not gateway inference endpoints.

The process creates a private RPM scope for each external request. Each
applicable counter is incremented at most once within that scope. A later
billing check reuses the actual increment result, including a failure, and
compares it with its applicable limit. Client input cannot create or reuse this
private state.

- Same-group account failover, HTTP retries inside one admitted request, and
  repeated billing checks reuse its user and user-group count.
- The configured prompt-too-long fallback keeps the same user count and checks
  the target group's own counter and override. The original group's cached
  exemption cannot authorize the fallback group.
- Every Responses WebSocket `response.create` turn is an independent request.
  The first turn uses its ingress scope; later turns create fresh scopes and
  read the current same-user, same-key global RPM policy through the auth
  snapshot, including when the key has moved groups or has no group. A
  compatible group refreshes its policy and user-group override. A changed or
  missing group/platform/subscription retains the connection's billing group
  and clears the cached override, so the original group's own override is
  queried. A different key/user identity or a failed lookup retains the existing
  connection snapshot. Earlier accepted turn snapshots are not mutated.
- An asynchronous submission counts once. Its trusted detached execution
  carries the admission scope so its internal forwarding does not count the
  submission again. An independent status or content request is a new ingress.
- Live and Grok Realtime connection admission counts once. Each Live Sideband
  reconnect is another audited ingress request and counts once, while reusing
  the persisted Live execution lease instead of acquiring a second user slot.
  Audio/control frames still cross their security-audit boundary, but do not
  each increment RPM.

An HTTP RPM rejection uses HTTP `429`, the existing
`USER_RPM_EXCEEDED` or `GROUP_RPM_EXCEEDED` error, and `Retry-After`. A later
Responses WebSocket turn failing this billing-eligibility check closes before
that turn is sent upstream. Account selection, upstream writes, and billing
side effects cannot follow a rejected RPM admission; any temporarily acquired
user slot is released.

## Existing policy boundaries

Simple mode retains its explicit bypass of user and user-group RPM. Its
optional API-key monetary-window check does not enable RPM. The API-key total
quota and `5h` / `1d` / `7d` usage windows are USD limits, separate from request
counts and execution concurrency.

Redis RPM errors retain the existing fail-open policy: log the failure and
allow the request to continue. An override-repository error falls back to the
group default. These policies do not guarantee a hard RPM ceiling while their
dependencies are unavailable. Execution-concurrency acquisition and renewal
retain their separate fail-closed behavior.

Security audit always precedes RPM reads/increments and override lookup. A
known empty metadata/control payload still enters the canonical audit gate;
unextractable valid content retains its pass-through behavior. RPM failure is
an admission error, not a content-policy decision. See the
[audit ordering contract](SECURITY_AUDIT_CONTENT_COVERAGE.md#boundary-and-ordering).

## Regression ownership

Service-level scope, concurrent duplicate checks, fallback counts, changed
limits, Redis failure behavior, and simple-mode behavior are specified in
`backend/internal/service/rpm_admission_test.go` and
`backend/internal/service/billing_cache_service_rpm_test.go`.
`backend/internal/handler/gateway_rpm_admission_test.go` specifies actual
Messages fallback forwarding, group-policy isolation, and forbidden billing
side effects. `backend/internal/handler/openai_gateway_ws_rpm_admission_test.go`
specifies both Responses WS ingress modes, separate turn counts, changed user
limits (including moved and ungrouped keys), and removed group exemptions before
a new upstream write. Test presence
does not establish a passing validation run; report the actual container checks
and their results separately.
