# Error request diagnostics

Error records distinguish request refusals, account selection failures, provider
failures and internal dependencies. Client protocol envelopes remain separate
from this internal attribution.

## Local security audit refusals

A trusted local `DecisionBlock` with HTTP 403 and one of
`content_policy_violation`, `session_blocked_by_content_policy` or
`prompt_guard_blocked` is stored as phase `request`, owner `client`, source
`client_request`, and that exact code as its internal type. Usage lists and
category filters expose `security_audit` (Security audit refusal / 安全审计拒绝).
Provider responses using the same strings do not establish local attribution.
Audit dependency failures retain internal/dependency attribution.

This correction preserves the existing business-limited/SLA flag. It does not
change the audit decision, wire envelope, billing, or admission ordering.
Rejected requests have no selected account or upstream attribution. WebSocket
errors snapshot the decision and timing per turn; a later audit refusal cannot
rewrite an earlier provider failure.

Migration 279 corrects only historical 403 records with the old
`internal/api_error/platform/gateway` classification, no account or upstream
status/message/detail/model/attempt evidence, and a valid JSON error carrying an
exact local refusal code with an expected local envelope type. Invalid or
ambiguous records are unchanged. Historical endpoint labels were derived even
before selection and do not prove an upstream call. Bodies, timestamps,
business-limit flags and timing are preserved; unknown TTFT is never backfilled.
There is no runtime interpretation of legacy rows.

## Local outbound header policy

A trusted project-identifier rejection at the final send boundary is phase
`internal`, owner `platform`, source `gateway`, with event stage
`outbound_policy`. Its stable reason is `protected_header`, `protected_trailer`
or `signed_declaration`; no header values or credentials are stored in the
message. It does not trigger provider cooldown, proxy failure accounting or
account failover. WebSocket records use the saved event for each turn, so local
and provider failures cannot relabel one another on a long-lived connection.
Routing Host is exempt; other header privacy remains enforced.

## Account selection

`routing_diagnostics` contains server-defined values collected by the scheduler,
not values parsed from error strings. Wrapped errors preserve the diagnosis and
compact/general sentinel used by admission logic.

Selection producers supply their decision and observed counts as one complete
snapshot. Transport and identity producers update only their independent fields;
they cannot supply or infer a missing selection decision.

- `candidate_pool: 0` means a measured empty pool. Missing or null means the pool
  was not observed. Negative counts are invalid and discarded. A new selection
  snapshot cannot borrow counts from an earlier result. The error detail displays
  these states separately.
- `filtered_candidates` counts actual known exclusions, including model,
  capability, runtime, quota and capacity checks. Compact exclusions and Grok
  quota gates retain the observed pool and their actual reasons. It is not an estimate of which
  setting an operator should relax.
- `selection_layer: channel_pricing` with
  `selection_reason: channel_pricing_restricted` means rejection before listing
  accounts; the candidate pool is unknown.
- Selection exhaustion keeps known pool/filter counts. These fields do not
  alter scheduling, model restrictions, account caps or quota policy.

## Body reads

`gateway.request_body_read_failed` logs a safe request ID, canonical endpoint,
protocol, `stage` (`read`, `decode`, `normalize`), stable reason, `body_bytes` and
`declared_body_bytes`. Unknown declared length remains `-1`.

Read-stage bytes include bytes returned alongside the failure. Decode-stage
bytes count the compressed wire body; normalize-stage bytes count decoded
input, while declared bytes retain the original declaration. The public error
text uses server vocabulary. Cancellation and size errors remain unwrap-able.
The log does not contain raw input, underlying error text, credentials, query
parameters or model path fragments. No partial body is returned to audit,
routing, billing or upstream forwarding. Read/decode failures remain HTTP 400;
size limit failures remain HTTP 413, including decompression limits.

## Partial stream failures

An upstream can flush HTTP 429 headers and leave a short body open indefinitely.
A byte limit does not bound that read, and a response-header timeout has already
finished. A detached inference context can therefore retain a connection,
execution slot and renewal work even after the client disconnects. Account
queue limits must remain enforced across different users while that upstream
is slow; user concurrency alone does not bound a shared account's waiters.
The isolated regression responders reproduce this resource-retention mechanism;
they do not establish the cause of an earlier outage without its runtime evidence.

Unsuccessful upstream HTTP responses have a five-second diagnostic-body budget
from response headers, in addition to their byte limit. The production transport
covers compressed-body initialization and subsequent reads within that same
budget. Service readers also enforce the bound for independent clients. This
covers generic forwarding, OpenAI and Chat Completions adapters, native image
download errors, Gemini Messages, Antigravity and Responses WebSocket HTTP bridging.
Gemini/Vertex Batch API error readers apply the same bound to their independent
HTTP clients while preserving typed provider status and diagnostics. Plugin
transport responses with HTTP errors have their own five-second body budget:
expiry cancels that RPC, closes its pipe and finishes its runtime request once,
including consumers such as input-token counting and Live creation that read
the returned body directly. EOF and caller Close stop the plugin timer; accepted
plugin streams retain their existing streaming lifetime.
Receiving an error status or a small diagnostic prefix without EOF cannot keep
an account connection and execution lease occupied indefinitely. The budget
closes the actual body and interrupts its active read before retry/failover;
already received diagnostic bytes and the HTTP status remain available for
the existing error classification. A client disconnect does not remove this
budget from a detached upstream request. Successful inference streams retain
their existing completion and usage-collection lifecycles.

The shared upstream transport starts this budget when rejected response headers
arrive. It includes gzip header decoding and zstd frame probing before the
service receives the body, plus independent control and media readers. EOF or
Close stops the timer. Expiry cancels that transport attempt, keeping its HTTP
status available while allowing the account connection to be released. It does
not cancel the caller or prevent another account attempt.
This is a rejected-body deadline, not a five-second deadline for the complete
request: connection/header waits, existing bounded retry/backoff policies and
accepted inference streams retain their separate budgets.

Observed text/reasoning/tool TTFT remains available on failed partial responses.
Metadata, keepalive and media-only output do not fabricate a token timestamp.
OpenAI upstream attempt JSON records `semantic_output_committed`, optional
`time_to_first_token_ms`, and `replay_suppressed_reason` when committed output
prevents safe replay. Attempt and WebSocket-turn snapshots do not inherit timing
from another credential owner. These fields do not change TPS calculations or
make partial usage complete.

Explicit encrypted-history recovery follows the
[Responses protocol](protocols/OPENAI_RESPONSES.md#encrypted-history-recovery).
Recovered attempts remain upstream health telemetry; a successful recovery does
not create a final failed-request record. Generic upstream 502, remote WS 1012,
HTTP/2 failures and post-output non-replay retain their existing decisions.
