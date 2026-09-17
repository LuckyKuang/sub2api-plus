# OpenAI Responses and WebSocket Ingress

Sub2API Plus accepts OpenAI-compatible Responses requests over HTTP and
client-facing WebSocket ingress. Account routing can use an upstream WebSocket
or bridge the client WebSocket to an HTTP/SSE upstream.

Usage timing follows the shared [first-token/total-duration/TPS contract](../USAGE_TIMING.md)
(TPS is decode rate over last-token minus first-token), including HTTP
passthrough, individual WS turns, and remote compaction. Aggregate
compact output does not create a token clock. The obsolete `openai_ttft_mode`
admin setting has been removed; `timing_version` identifies verified usage data.

## GPT-6 Astra

The gateway uses OpenAI's canonical `gpt-6-astra` model ID. For Plus client
compatibility, the legacy `gpt-6` spelling is accepted only as an alias and is
canonicalized to `gpt-6-astra`; it is not a separate model or billing identity.
Reasoning-level suffixes are never treated as model IDs.
Astra accepts `low`, `medium`, `high`, `xhigh`, and `max` reasoning effort;
it does not accept `none` or `minimal`. Configured Codex catalogs default it
to `low` and preserve `max` as distinct from `xhigh` in request and usage
metadata. The additional Codex catalog level `ultra` is a client-side
multi-agent preset that delegates with `xhigh`; it is never sent upstream as
an API reasoning effort.

Generated API client configuration advertises the 1,050,000-token API context
window and 128,000-token maximum output. Configured Codex catalogs follow the
Codex client contract separately: the active context window is 272,000 and the
maximum configuration override is 872,000.

Default OpenAI model catalogs and model-mapping presets list Astra first. The
admin account test UI still prefers `gpt-5.6-sol` when it is available, so
catalog presentation order does not silently change the connectivity probe.

Default API cost accounting uses $10 input, $1 cached input, $12.50 cache
write, and $50 output per million tokens. Flex is half of Standard and Fast is
twice Standard. OpenAI publishes Batch at half of Standard through its separate
Batch API; this Responses gateway does not infer Batch from `service_tier`.
When total input is 272,001 tokens or more, the whole-request long-context
policy multiplies all ordinary input, cache-read input, and cache-write input
by 2 and all output by 1.5. The 272,000-token boundary retains Standard rates.
Platform API-key Astra requests always apply this official cost policy;
ChatGPT OAuth account policy remains independently configurable. Group and
channel custom selling-price overrides retain their existing precedence.

Astra tool calling requires the Responses API. Callers must omit
`temperature`, `top_p`, and `top_logprobs`; Chat Completions callers must also
omit `logprobs`, and Responses callers must not request
`message.output_text.logprobs` through `include`. Fast/priority processing is
not available with EU data residency and does not carry a latency SLA.
Responses features such as async tool calls, `configuration_update`, and
mid-turn WebSocket steering pass through the gateway without Astra-specific
rewriting.

## Prompt Cache Identity and Usage

Current Codex clients can supply the canonical `session-id`, `thread-id`, and
`x-client-request-id` headers. The gateway also accepts the supported legacy
session aliases, including `session_id`, for sticky routing compatibility.
When direct session headers are absent, the stable string `session_id` inside
`X-Codex-Turn-Metadata` is also accepted; request-scoped `turn_id` is never a
routing key. This lets `/v1/responses` WebSocket ingress and
`/v1/alpha/search` share one account route even when Alpha Search would
otherwise fall back to its search ID.
The same sanitized value is recorded as `usage_logs.session_id` and participates
in cyber-session blocking. Endpoint fallbacks and `turn_id` remain routing
details and are not persisted as client session IDs.
Thread and client-request identifiers remain paired request context and never
replace the session-scoped cache identity.

For OpenAI Responses and Compact requests, the gateway resolves one opaque,
tenant-isolated cache identity from an explicit `prompt_cache_key`, a supported
session header, or a stable content prefix with a meaningful user/input anchor.
It writes the finalized UUID to both the upstream `prompt_cache_key` and the
canonical `session-id` header. Official Codex never sends a `conversation_id`
header, so Codex-protocol outbound requests never carry one. The legacy
`session_id` alias is a Plus compatibility header: OAuth accounts emit it only
when the fingerprint mode converges session identity (`session` or `full`),
while API-key accounts keep emitting it. `off` and `device` OAuth accounts keep
the official `session-id` + `thread-id` spelling. A model-only request does not receive a content-derived key. API-key
Chat Completions requests converted to Responses use the same behavior, while
raw Chat Completions forwarding does not receive Responses-only cache fields.

Under the default hard-affinity mode, account priority changes do not replace a
valid active session route. The optional sticky-weighted scheduler mode remains
score-based by design. If the configured health/concurrency sticky escape
temporarily bypasses a degraded account, a movable Responses continuation does
not fall back to that account through its older response ID; the temporary
candidate order is derived from the shared session identity, while the
canonical sticky binding remains unchanged. Removing an account from the
requesting group, disabling it, or making it incompatible is always a hard
invalidation: both ordinary sticky routing and `previous_response_id` routing
recheck the current account before reuse. Long-lived Responses WebSocket
connections also run current billing and reload the selected account before
every turn in `ctx_pool`, `http_bridge`, and `passthrough` modes. The refreshed
account must still satisfy group, status, schedulability, quota, client-model,
endpoint-capability, transport, parent-health, runtime-block, scheduling
threshold, and proxy-quarantine gates. Follow-up image intent also repeats the
group permission and Responses-capability checks. If any gate fails, the
gateway closes before sending that turn upstream; a reconnect then performs
normal account selection. The first turn is billed-eligible once before
selection, while every later turn is checked once at its pre-turn boundary. A
movable WebSocket continuation follows a session route that has already failed
over to another account and removes the old account's response ID before
forwarding. Tool-output continuations without complete call context remain on
the response owner and keep the existing fail-closed OAuth ownership checks.

`prompt_cache_options` is forwarded only for GPT-6 Astra and GPT-5.6-family
OpenAI Platform API-key Responses/Compact traffic. ChatGPT OAuth and older or
unknown model families have that field removed. Deprecated
`prompt_cache_retention` is removed on every path. Current callers should use
`prompt_cache_options.ttl` with the only supported value, `30m`; setting
`mode` to `explicit` disables the implicit cache breakpoint.

Usage ingestion treats ordinary input, cache-read input, and cache-write input
as mutually exclusive stored buckets. Usage pages may report each bucket's
share of total tokens, but do not derive a prompt-cache hit rate from these
counters.
Canonical nested usage details take priority by field presence, including an
explicit zero, before known top-level compatibility aliases are considered.

## Codex Scheduler Quota Windows

OpenAI account scoring reads canonical `codex_5h_*` and `codex_7d_*` usage
fields first. Historical `codex_primary_*` / `codex_secondary_*` snapshots reuse
the same Normalize window classification used when writing extras, so `primary`
is never assumed to be the 7-day window. Reset scoring prefers the canonical
5-hour reset time and falls back to the session window. Relative
`reset_after_seconds` is anchored at `codex_usage_updated_at`; a missing sample
time does not slide the countdown forward on each score. Scheduler weights,
pause thresholds, and Plus session/quota accounting are unchanged.

## Upstream Capacity Shed

OpenAI capacity-shed signals (`server_is_overloaded`, `slow_down`, and messages
such as `Our servers are currently overloaded` or `Selected model is at
capacity`) are request-scoped. The gateway does not retry the same account and
does not switch accounts. The first upstream response is returned to the
client as HTTP 503. Account health, scheduling, and pause state are unchanged.

When the error is forwarded, `server_is_overloaded` / `slow_down` are rewritten
to `server_error` so Codex CLI does not treat them as a fatal session-ending
code. The original message is preserved. Rate-limit codes are not rewritten.
Codex WebSocket HTTP-bridge and native WS turns deliver that rewritten event
on the current connection instead of switching accounts or closing with a
generic proxy failure.

Anthropic `overloaded_error` / HTTP 529 and Grok shared model-capacity errors
follow the same no-retry rule for the current request.

Transport failures, account-scoped 401/403/429, and OAuth 429 windows keep
their existing retry and failover behavior.

## Responses Stream Sequence Numbers

Gateway-synthesized and re-emitted Responses SSE frames always write
`sequence_number`, including `0`. Compact HTTP/SSE bridges number events
monotonically from `0`. Synthetic `response.failed` frames and WebSocket-to-HTTP
bridge error/`response.failed` events emit `0` when the previous sequence is
unknown. Strict clients such as Grok Build treat the field as required and abort
the turn when it is omitted. The OpenAI spec marks it optional; Plus still emits
it so those clients can deserialize the stream.

## Codex Rate-Limit Response Headers

For Codex Responses requests, successful HTTP and SSE responses can include
the following rate-limit fields for both `Primary` and `Secondary` windows.
WebSocket upgrade responses include the same fields when the local Codex
subscription quota view is enabled:

- `X-Codex-*-Used-Percent` is the consumed percentage.
- `X-Codex-*-Window-Minutes` is the window length in minutes.
- `X-Codex-*-Reset-At` is the next reset time as Unix seconds.
- `X-Codex-*-Reset-After-Seconds` remains alongside `Reset-At` for older
  clients that only understand a relative countdown.

When local Codex subscription quota is enabled, `Primary` represents the local
7-day window and `Secondary` represents the local rolling 5-hour window. This
local view is authoritative even when the selected OpenAI account enables
automatic passthrough. The gateway clears all upstream default rate-limit
fields before writing the local values, so one response never mixes upstream
reset times with local percentages or window sizes.

When the local view is disabled, the selected account's real default quota is
visible only when that account enables OpenAI automatic passthrough. Otherwise
the gateway removes the default `Primary` and `Secondary` quota fields after
generic response-header filtering, so an additional response-header allowance
cannot bypass this policy.

A client WebSocket `101` response is committed before the gateway connects to
the selected upstream. The gateway therefore writes only the local quota view
known before the upgrade. The official WebSocket client updates quota from
in-band `codex.rate_limits` events, so the gateway applies the same policy to
the default `codex` event family: replace it with local subscription windows,
pass it through for an automatic-passthrough account, or suppress it. Named
model-specific limit families remain independent. HTTP and SSE headers are
finalized before their response bodies are written.

The dedicated `/backend-api/wham/usage` route remains a local-only view. It
returns the API key subscription quota when the local setting is enabled and
returns 404 otherwise; it does not select an account or proxy upstream quota.

Administrators may optionally bind an OpenAI subscription group to one
credential-owning OpenAI OAuth account that is already bound to that group.
This does not change the group's routing pool. Raw default weekly observations
from real inference HTTP traffic (including compatible endpoints) and
`codex.rate_limits` WebSocket events share one observation path, before
client-facing local quota rewrites. WebSocket connection handshake headers
may refresh the account usage cache, but they are a connection-time snapshot
and never drive this feature. Account usage probes, standalone quota
queries and reset-credit workflows never drive this feature. Only an
explicit 10,080-minute / 604,800-second default window with a raw absolute reset
time is eligible. Daily, 5-hour, monthly, unknown and model-specific/Spark windows
never drive this feature. Display countdowns, expired-cache synthetic zeroes,
reset-credit expiration dates and local account A/U costs are not reset signals.

No background worker queries OpenAI for this feature. A reset is evaluated only
when an eligible source account returns a fresh official weekly window during a
real inference session. Local pending events are still processed in the
database, but processing them never performs an upstream request.

Saving a new binding, re-enabling a disabled binding or changing its source locks
the source and clears the group's old baseline. Its first fresh confirmed window
only establishes a baseline; it never clears subscriptions, even if the account
retains an old deadline or pending reset evidence from an earlier activation.
Only a real inference session sampled after activation establishes that baseline;
standalone quota queries and samples observed at or before activation cannot
establish it. Sample time is the HTTP response or WebSocket event time, so a
request that started before activation and completed after it can establish the
baseline. While an off-schedule window is awaiting confirmation, new bindings keep waiting.
Disabling clears the baseline and advances the configuration generation, making
old pending events inapplicable. Upgrades preserve continuously enabled bindings
and existing subscription counters; the migration performs no quota resets.
Migration `265_openai_weekly_reset_observations.sql` changes the event uniqueness
key to include the reset sequence. Deploy it together with the updated backend;
backend instances sharing this database must not mix old and new event writers.
Ordinary group edits, including copying members while retaining
the same reset source, preserve the current baseline and configuration generation; stale
source-configuration saves are rejected for reload. Natural rollover can be
accepted when the prior deadline has expired and the next deadline advances by
more than five minutes. Early deadline changes in either direction, and usage
drops of at least one percentage point with an unchanged deadline, first persist
pending evidence. A separate real inference session sampled at least one second
later and within ten minutes must corroborate it. Deadline changes within five
minutes are treated as clock drift. Stale/out-of-order samples are ignored.
A sample cannot confirm or postpone its own pending evidence. Confirmation
requires two real inference samples: live HTTP `/responses` (and compatible)
headers, or in-band `codex.rate_limits` events. Reparsing the same WebSocket
handshake headers at a later turn cannot corroborate or dismiss pending
evidence. Account usage-cache writes also compare sample times under
the account row lock, retaining subsecond precision: older or duplicate samples
cannot replace newer usage percentages or deadlines. Unrelated account settings
in the same update still merge; missing or invalid historical timestamps do not
prevent fresh snapshots from being stored. No zero-percent observation is required: the first observed
post-reset usage may already be 7% or higher. A first-ever same-deadline reset
cannot be inferred without a prior utilization sample.

Confirmed resets have a durable monotonic sequence, so two real resets with
the same announced deadline remain distinguishable while refreshes are idempotent.
Each event resets the active subscriptions' 5-hour, daily, and weekly usage;
monthly usage is reset only when the group explicitly enables it and has a
monthly limit. The local confirmation time starts the new subscription windows;
quota limits are unchanged. Historical consumption is not replayed or backfilled
from account A/U statistics, and account usage logs remain intact. New charges
after the reset accumulate normally. Event application and usage billing lock the group and
subscription records in the same order, so a concurrent charge is
deterministically ordered before or after the reset. Deleting the source
account, or removing it from the group, leaves its recorded name and ID on the
group for diagnosis, but disables further automatic resets until another
eligible bound source is selected.

Source membership is checked when creating reset events and again when either
billing or the local event applier applies them. An unbound source neither advances
the group's baseline nor clears usage through a previously pending event.
Observation and event-application paths acquire the group lock before rechecking
membership in a fresh statement, so a lock wait cannot retain pre-edit membership.
Event application also refreshes source eligibility before applying any reset.

An accepted window repeated by the source still establishes missing group
baselines without resetting them. A behind baseline on a continuously enabled
group can be reconciled after natural rollover. Synchronized groups do not create
duplicate events. Opening the group editor reloads its current server detail,
including the baseline, rather than reusing an older list-row snapshot.
While the editor remains open, its baseline/status refresh every 15 seconds
without changing unsaved settings; refreshes stop when it closes.
Async editor detail, routing and manifest-name results are discarded after a
new edit selection, closing the editor or leaving the page.

Creating or editing a group with copied accounts commits its configuration,
membership and scheduler outbox entries together. A failed copy rolls back the
entire write. Copying an unchanged source cannot temporarily expose an unbound
source to reset workers. Copied accounts are locked before the group, in ID
order, matching observation lock ordering and avoiding membership-FK deadlocks.
The same account-before-group order applies to single-account additions,
replacement of an account's group bindings, and batch membership inserts.
Creating a Spark shadow with group bindings also locks the existing credential
parent before groups, since the new account's parent foreign key locks that row.
Membership writers lock all affected existing accounts in ascending ID order
before acquiring live-group locks or inserting membership foreign keys. This
also applies inside a caller-owned transaction; callers that edit groups before
binding members must prelock the complete account set at transaction entry.
Weekly observations and membership writes can wait for one another without
reversing that order, and a rollback retains the original group bindings.

Multiple workers process each group's pending events in order, preserving
earlier monthly-reset decisions. Billing consumes eligible pending events
before adding the request's cost, including inside a caller-owned transaction;
subsequent worker passes cannot erase that cost. Pending events with deleted,
ineligible, disabled, or replaced sources are ignored by both paths. Persistence
of an already received upstream observation uses a bounded five-second context
independent of downstream cancellation and makes no additional upstream call.

This response-header compatibility does not make Codex App API-key calls to
`account/rateLimits/read` available; that App Server authentication behavior is
outside this gateway's request path.

## Codex Fingerprint Convergence

OpenAI OAuth accounts may rewrite outbound Codex installation, session, and
thread carriers. Unset accounts use `device` mode. Ordinary Responses,
Chat-Completions-to-Responses, Messages-to-Responses, HTTP-to-WebSocket, and
direct Responses WebSocket turns use the configured account mode. Native
remote Compact v2 is an ordinary Responses session for fingerprint purposes
and therefore also uses the full configured mode. The ChatGPT Codex OAuth
legacy compact compatibility path uses installation-only convergence for every
non-`off` mode and preserves its own compact session, cache, and thread
namespace.

Here `legacy` refers only to the ChatGPT Codex OAuth compatibility branch used
by this gateway. The public API-key
[`/v1/responses/compact`](https://developers.openai.com/api/reference/java/resources/responses/methods/compact)
endpoint remains a distinct supported OpenAI API surface. Response retrieve,
cancel, and other non-create subpaths are not session turns and receive no
fingerprint mutation.

Fingerprint preparation runs before final request construction. Plus
prompt-cache/session isolation is authoritative for the final `session-id` and
`session_id` headers, while fingerprint convergence remains authoritative for
installation and thread/turn metadata. `off` disables only fingerprint-owned
header and body mutation; it does not disable Plus cache isolation, security,
session sharing, or compact policy. WebSocket connection reuse compares final
stable handshake carriers even when `off` or `device` leaves those values
client-owned. Usage-log `session_id` stays the sanitized client-original value.

Only credential-owning OpenAI OAuth accounts participate. Personal access
token and Agent Identity accounts follow the same endpoint semantics because
they are OpenAI OAuth credential owners. API-key, setup-token, and non-session
endpoints such as count-tokens and alpha-search are excluded. User-Agent,
Originator, and Version use one source chain: valid credential-owner
`credentials.user_agent`, then valid global `openai_codex_user_agent`, then the
compiled default. Version synchronization changes only the version declaration
of the selected identity.

## Security Audit Content Boundary

Inbound Responses content is normalized for Content Moderation and Prompt
Audit before account selection, billing, concurrency acquisition, fingerprint
convergence, request adaptation, or upstream writes. API-key and OAuth account
paths therefore use the same audit content.

The canonical boundary covers top-level and `response`-nested `instructions`,
`tools`, `input`, reusable `prompt.variables`, message text, tool definitions,
and the arguments, input, output, result, or dynamic tools carried by function,
custom, tool-search, local/hosted shell, apply-patch, computer, MCP,
code-interpreter, and programmatic-tool-calling items. Media fields and
encoded screenshots are removed before text serialization and persistence;
ordinary text in the same structured result is retained.

Content Moderation consumes the same canonical result but selects only the
current direct-user message text and images. It excludes `instructions`, tool
definitions, reusable prompt variables, assistant/model messages, reasoning,
tool calls/results, approval responses, and tool-produced screenshots. This
prevents platform context or external tool content from being reported as a
user policy violation. Prompt Audit consumes the same current direct-user text
and does not scan images. It excludes `instructions`, tool definitions,
reusable prompt variables, assistant/model messages, reasoning, and tool
calls/results. Client harness XML inside user text may be stripped before the
Guard scan. A turn with no current user text is an empty Prompt Audit
selection.
A supported WebSocket control frame may produce no audit input. Unknown sibling
keys, unsupported event/item types, and valid-JSON unrecognized structures pass
through without an audit-derived block. When the canonical extractor recognizes
`input`, `instructions`, or nested `response.input`, an envelope `type` value
does not suppress those extracted segments. An unsupported envelope type is
still counted and safely logged as an extraction failure while those extracted
segments remain auditable.
Non-empty root, nested `response`, and session objects with no recognized field
are counted and safely logged as extraction failures before they pass through;
unknown sibling keys on an otherwise recognized object remain ordinary success.
Direct passthrough runs the audit hook for every client text or binary frame,
including `conversation.item.create` and `session.update`, before any
non-`response.create` frame is forwarded. Unsupported binary/JSON content and
recognized items that cannot be normalized are logged and pass through unless
independent transport/basic validation rejects them. Successfully extracted
sibling content remains auditable. Extraction failure alone never becomes a
policy block, unavailable decision, HTTP 503, or WebSocket close. Compact
keepalive output and channel mapping start only after this gate. The audit uses
an immutable copy of the inbound body so compact normalization and reasoning
policy rewrites cannot remove content from the audited view.

The complete protocol/source matrix is maintained in
[`docs/SECURITY_AUDIT_CONTENT_COVERAGE.md`](../SECURITY_AUDIT_CONTENT_COVERAGE.md).

## Request Replay and Upstream Failures

When a Responses request replays a previous tool call, the gateway preserves an
item `id` only when its prefix matches the item type. In particular,
`custom_tool_call` uses `ctc...`; a mismatched item ID is removed rather than
rewritten. Its `call_id` remains unchanged so the paired
`custom_tool_call_output` continues to reference the original call.

The exact local proxy response `507` with
`exceeded request buffer limit while retrying upstream` is not an account or
model failure. The gateway stops replaying the request, keeps the selected
account eligible, and returns an OpenAI-compatible `413` with:

```text
Request payload is too large to retry safely
```

Reduce the request size or adjust the reverse-proxy retry-buffer policy before
retrying. The gateway does not retry that request through another account,
because doing so can duplicate work and billing while encountering the same
buffer limit.

Connection refusal/reset and HTTP `504` gateway failures are recorded as
provider or proxy transport failures rather than model-capacity failures. A
proxy-backed OpenAI account is isolated by the bounded proxy circuit, keyed by
the configured proxy ID; a shared proxy incident therefore does not directly
put every associated account into an account-level cooldown. Management error
records retain only structured, bounded diagnostic categories and never store
raw proxy URLs, credentials, or outbound User-Agent values.

For the native HTTP/SSE and WebSocket paths, diagnostics distinguish an edge
gateway timeout from the gateway's own response-header, first-semantic-output,
and WebSocket first-semantic-output deadlines. A first-output timeout may use
the existing single, pre-output controlled failover path; it is never enabled
after semantic output has reached the client.

## WebSocket Ingress Limits

`gateway.openai_ws` bounds the lifetime and aggregate count of client-facing
sessions independently from per-turn user and account concurrency:

```yaml
gateway:
  openai_ws:
    client_first_message_timeout_seconds: 30
    ingress_inter_turn_idle_timeout_seconds: 300
    max_ingress_connections_per_api_key: 64
```

- The first-message timeout covers receiving and decompressing the complete
  first client message.
- The inter-turn timeout closes idle sockets after a completed turn; `0`
  disables it.
- The API-key connection cap is distributed through Redis; `0` disables it.

The connection lease is acquired after the first frame passes basic validation
and security audit, before user/account concurrency, billing and upstream work.
An upgraded socket waiting for its first frame is bounded by the first-message
timeout and does not yet consume a Redis lease. Capacity exhaustion or an
unavailable lease backend closes the upgraded socket with code `1013` (try again
later); clients should reconnect with backoff. Audit rejection retains its own
error frame and close status and never consumes a connection lease.

Large contexts or slow image-heavy requests may require a higher first-message
timeout. The timeout expires before HTTP bridge routing and is not overridden
by bridge mode.

Distributed connection leases last 60 seconds and refresh every 20 seconds. If
a process cannot confirm a lease for a full lease lifetime, it closes the local
socket instead of continuing outside the global cap.

## Mode Router

Enable the v2 mode router before selecting an account WebSocket mode such as
`http_bridge`:

```yaml
gateway:
  openai_ws:
    mode_router_v2_enabled: true
```

The environment equivalent is
`GATEWAY_OPENAI_WS_MODE_ROUTER_V2_ENABLED=true`. Use `http_bridge` when the
client keeps a WebSocket while the selected upstream uses HTTP/SSE.
