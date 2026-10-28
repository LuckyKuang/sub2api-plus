# Gateway task admission

Gateway execution concurrency and unfinished asynchronous work are separate
limits. The execution pool is shared by user across API keys and providers;
independent main-agent and subagent requests each need admission. An idle
provider queue does not keep an execution lease. Short poll/cancel requests
remain able to acquire execution capacity while the provider is working.

`gateway.max_unfinished_tasks_per_user` is a positive persistent per-user cap,
default 100. `GATEWAY_MAX_UNFINISHED_TASKS_PER_USER` overrides YAML. Batch images
(Gemini and Vertex), asynchronous image jobs, Grok video and Seedance share this
one capacity pool, including across instances and API keys. Lowering the cap
does not cancel accepted tasks; it prevents new submissions until usage falls
below the cap. PostgreSQL failure returns `UNFINISHED_TASK_UNAVAILABLE`; a full
pool returns HTTP 429 `UNFINISHED_TASK_LIMIT` before provider creation.

The SQL repository owns `gateway_unfinished_tasks`. A reservation locks its
user row and checks/inserts under one transaction. The creating operation first
reserves capacity, persists `uncertain` before sending a provider create, then
binds the provider/local task ID when its result is known. A definite failure
before create releases the reservation. A successful terminal observation or
confirmed cancellation releases it idempotently. Billing retry and task
capacity are independent; zero billable tokens do not imply a finished task.
If persisting a Batch create intent fails, no provider call is made: the service
immediately releases its balance hold with an independent cleanup budget and
marks the local submission failed and hidden. A failed refund retains the
existing billing-release retry path, while the provider-task reservation is
returned because execution never started. A concurrent cancellation retains
its committed terminal status and visibility; failure cleanup cannot overwrite
it or hide the cancelled task.

For Gemini/Vertex batches, observing provider success releases capacity before
result download/indexing and settlement. Output retrieval failures and billing
retries retain their result or balance-hold state without keeping provider-task
capacity occupied. Confirmed success, failure, cancellation or expiry also frees
capacity before output-reference persistence, so a failed local write cannot
keep completed provider work counted. A restarted worker also recovers capacity
from the durable `indexing` or `settling` state, which is entered only after
provider completion.
Recovery is scoped to the original owner and task; it cannot release a newer
task submitted through another API key. Queued, running and unconfirmed provider
states still consume capacity.

Grok/Seedance forwarding preserves explicit local no-create evidence in its
error chain when account, credential, URL, body or request preparation fails
before the create transport is called. The lifecycle handler can then release
the reservation while keeping the original client error and scheduling
classification. Once the transport is called, a timeout or response-read failure
does not supply that evidence and keeps capacity occupied.

Grok and Seedance deferred billing uses a separate 30-second Redis processing
lease with a random owner. A crashed or abandoned worker can be retried while
the accepted pricing evidence still exists (24 hours). Durable billing success
replaces only that owner's lease with a 48-hour completion marker; errors or
panics release only the matching owner. Completion and release have an
independent two-second budget, including after the billing worker times out.
An expired owner cannot delete its successor or a completion marker. Stable
task billing request IDs and PostgreSQL billing deduplication enforce one
charge if a slow worker and its successor both reach settlement. Processing
lease expiry does not release unfinished provider-task capacity.
The processing keys use a new `grok_video_billed:v2:` namespace: old `"1"`
values cannot distinguish an interrupted pre-commit attempt from success. The
new generation retries through the stable SQL request ID, recovering unpaid
tasks and deduplicating already paid tasks without rewriting old caches.

Unknown outcomes keep capacity. Connection loss, process death, missing task
IDs, Redis expiry and a local failed status cannot prove that the upstream task
stopped. There is no time-based release. An uncertain record only releases on
confirmed provider termination/completion/cancellation or independent proof
that provider execution never started. A worker rejected before execution,
including by user admission or failure to persist its create intent, releases
its reservation; an ordinary local terminal marker after execution begins
cannot release it. Deleting an API key does not delete its
unfinished reservations. Records contain owner IDs, kind, task ID and stable
internal reason codes, without prompts, response bodies or credentials.

Migration 300 counts existing durable Batch and processing async-image tasks.
Forward migration 301 releases exact owner/task Batch reservations already in
`indexing`, `settling`, `completed` or `output_deleted`, so completed provider
work does not block new submissions while workers are disabled or delayed after
upgrade. It does not release local `failed`/`cancelled`, queued/running or unknown
create outcomes, and it never matches another API key, user or task kind.
Reservations above the configured cap remain accepted and block new creates.
Older locally failed tasks can also have an unknown upstream outcome; the
migration cannot reconstruct that evidence from a failure status. Include them
in the rollout reconciliation. The older Grok/Seedance Redis-only pending
records cannot be reliably enumerated from PostgreSQL: reconcile accepted
pre-upgrade tasks during rollout before
claiming a complete historical unfinished count. Pause legacy creates and drain
their known provider tasks before promotion, or inventory the pending IDs and
insert owner-scoped ledger bindings under the user lock before enabling new
creates. If the inventory cannot be proved complete, keep legacy creates
paused and investigate; starting with zero is not proof of zero unfinished work.
An isolated test responder does not prove real provider cancellation or completion.

## Reconcile uncertain work

Inspect active/uncertain rows by authenticated owner and reservation ID using
the database. Correlate the task's local job/history and provider operation ID;
check or cancel it through the supported owned task endpoint or provider
console. Keep the reservation while termination is unknown. A missing provider
ID requires provider-side correlation; elapsed time alone is insufficient.

A Grok/Seedance create can return a known provider task ID while persisting its
reservation binding fails. The `grok_media.unfinished_bind_failed` log includes
both `reservation_id` and `task_id`, with the authenticated owner in its request
fields. Such an unbound SQL reservation remains uncertain: later task polls
cannot release it by task ID or automatically repair the missing binding.
Correlate the exact reservation, owner and known provider task, confirm terminal
provider evidence, and reconcile that reservation below. A binding failure does
not imply that creation failed, and age is not a reason to free its capacity.

When independently verified that no provider work remains, an operator can
release exactly the inspected reservation in a transaction, scoped by its
reservation ID, user ID and API key ID:

```sql
UPDATE gateway_unfinished_tasks
SET state='released', reason='operator_confirmed_terminal',
    released_at=COALESCE(released_at,NOW()), updated_at=NOW()
WHERE reservation_id=:reservation_id
  AND user_id=:user_id AND api_key_id=:api_key_id
  AND released_at IS NULL;
```

The named values are placeholders for an operator's database client. Record the
provider evidence and change in the operations audit. Do not truncate the
table, expire active records or release every row for a user to clear a queue.
This reconciliation is an operational action and is not performed automatically
by startup or a timer.

Business regressions are owned by `gateway_unfinished_tasks_test.go` (service),
`batch_image_unfinished_lifecycle_test.go` (provider completion, result/billing
retry, restart recovery and shared-cap admission),
`gateway_unfinished_tasks_integration_test.go` (PostgreSQL serialization,
upgrade admission before worker recovery, owner isolation, uncertain binding
failure and exact reservation reconciliation), and gateway lifecycle handler regressions.
All verification runs in the repository validation container.
