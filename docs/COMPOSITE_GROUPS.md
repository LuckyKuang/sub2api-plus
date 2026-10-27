# Composite Groups

Composite groups are an admin routing layer for API keys that should choose a
concrete provider from the requested model instead of binding the key to a
single provider group. They support both built-in model detection and an
admin-configured model route registry for public model aliases.

## Supported Providers

Composite groups accept every concrete account platform registered in
`backend/internal/domain/platforms.go`, including Anthropic, Gemini, OpenAI,
Antigravity, Grok, Kimi, Zhipu GLM, DeepSeek, MiniMax, StepFun, OpenCode,
TypeSafe / Jev, Command Code and Cline. The same registry supplies route targets,
platform quota buckets, channel pricing keys and the built frontend platform
catalog. Newly registered platforms inherit this configuration support; their
protocol/authentication adapters and supported endpoints still require their own
implementation and regression evidence. Registration does not add a missing
native protocol, WebSocket transport or media API.

The selected concrete platform is used for account selection, user platform
quota checks, post-usage billing, ops error platform attribution, channel
mapping/pricing lookup, and platform usage reporting.

## Route Registry

Admins can configure routes on a composite group from the group list's
`Routes` action or through the admin API:

- `GET /api/v1/admin/groups/:id/composite-routes`
- `POST /api/v1/admin/groups/:id/composite-routes`
- `PUT /api/v1/admin/groups/:id/composite-routes/:route_id`
- `DELETE /api/v1/admin/groups/:id/composite-routes/:route_id`
- `POST /api/v1/admin/groups/:id/composite-routes/preview`

Each route belongs to one composite group and contains:

- `public_model`: model identifier the client sends.
- `match_type`: `exact` or `prefix`.
- `target_platform`: concrete provider platform.
- `upstream_model`: model identifier sent upstream. If omitted, the public
  model is reused.
- `endpoint`: `any`, `messages`, `count_tokens`, `responses`,
  `chat_completions`, `embeddings`, `images`, or `gemini`.
- `priority`: lower values win after match specificity.
- `enabled`: disabled routes are ignored by runtime resolution but remain
  visible to admins.

Resolution order is explicit route first, then unambiguous exact account model
ownership, then built-in detection. When more
than one explicit route matches, exact matches beat prefix matches,
endpoint-specific routes beat `any`, longer prefixes beat shorter prefixes,
then lower `priority`, then lower route id.

For JSON-body endpoints, the gateway rewrites the request `model` field to the
route's `upstream_model` before dispatch. For Gemini native paths such as
`/v1beta/models/{model}:generateContent`, the gateway resolves `{model}` and
the handler forwards the resolved upstream model.

Codex Alpha Search and Live requests use the `responses` route domain. Live
requests resolve the model from `session.model`, including multipart `session`
payloads, and apply the configured `upstream_model` before dispatch.
Codex model manifest requests reuse the existing OpenAI account selection and
failover path within the Composite group.

Gemini explicit context-cache creation uses the `gemini` route domain for the
JSON `model` field. Only Gemini targets support these resources. The returned
metadata retains the public model while upstream creation uses the resolved
model. List/get/extend/delete and generation references preserve the original
API-key, group and creating-account binding. Extensions use the current pricing
snapshot and the stored public model; changing an alias to another upstream
model cannot silently reuse the old cache.

Responses WebSocket resolves the first `response.create` model in the
`responses` route domain (including `any` routes). Only OpenAI and Grok targets
are supported. The public model remains the admission and response identity;
the route's upstream model is applied before channel/account mapping. Subsequent
turns can repeat or omit the public model. Changing it requires reconnecting so
the gateway can resolve the route and select an account again.

## Native supplier APIs

The existing Grok `/tts`, `/stt`, `/custom-voices` CRUD/audio, `/realtime`,
`/web_search` and `/x_search` APIs also accept Composite keys at both root and
`/v1` paths. These APIs fix the supplier to Grok by endpoint contract; their
native `model` parameters are not processed as text-route aliases. Account
membership remains in the Composite group, while security audit, account
selection, quota and billing use Grok as the concrete supplier. Standalone
search rates match the public `grok-web-search` and `grok-x-search` meters,
independently of the internal text model used to execute a search. Voice retains
its existing `tts`, `stt` and realtime model meters and duration/character units.
Ark/Seedance native task paths (`/api/v3`, `/v3`, `/v1` and root aliases) likewise
fix their supplier to the OpenAI-compatible Ark account pool before admission.
Their native model fields are preserved instead of being rewritten by text
Composite aliases; platform selling factors are validated and reserved before
creating a billable task, using the same supplier as deferred settlement.

## Built-In Detection

Composite routing detects common public model IDs and provider-prefixed IDs:

- `claude-*` and `anthropic/claude-*` route to Anthropic.
- `gemini-*` and `google/gemini-*` route to Gemini.
- `gpt-*`, `o*`, `codex-*`, `text-embedding-*`, `dall-e-*`, and
  `openai/*` route to OpenAI.
- `grok-*` and `xai/grok-*` route to Grok.
- `kimi-*`, `moonshot-*`, and `k3` route to Kimi.
- `glm-*` routes to Zhipu GLM.
- `deepseek-*` routes to DeepSeek.
- `step-*`, `step/*` and `stepfun/*` route to StepFun.
- `minimax-*`, `abab*`, and `minimax/*` route to MiniMax.

Unknown or ambiguous model names fail closed with a client error instead of
guessing a provider.

## Consumption rate policies and sale catalogs

All concrete suppliers share the user's execution concurrency and persistent
unfinished-task admission. Asynchronous upstream waiting does not hold an
execution lease; its separately persisted unfinished reservation remains until
confirmed termination. See [gateway task limits](GATEWAY_TASK_LIMITS.md).

A group can configure `billing_rate_policy.platform_multipliers` and
`billing_rate_policy.model_multipliers`. Model rules contain a concrete supplier
`platform`, a public request `model` (exact or trailing `*`), `multiplier`, and
optional `billing_unit`. The platform and matching model factors multiply;
each absent factor defaults to 1. Rules are evaluated
against the original public model and actual resolved supplier, never inferred
from a model-brand prefix after routing.

The platform × model product multiplies the existing customer/group rate. Personal group
rates continue replacing the group base and independent image/video rates retain
their existing base override. For new admissions, the applicable subscription
peak factor multiplies every charged meter, including independent media bases.
A zero factor remains free when the underlying unit price is known. Base unit prices, context tiers,
service tiers, reasoning and price-card time conditions keep their own billing
rules; a multiplier does not create provider capability or change billing units.
Editing a group's model cards preserves existing Fast/Flex price multipliers,
including cards that only override those service tiers. They remain separate
from the platform and public-model selling factors.

The request's business operation still chooses its existing base rate. An image
or video request with a `per_request` card keeps its independent image/video
base when enabled, while its model rule matches the `request` meter. Token-mode
media uses the token base. Native video duration is billed
in `second`, audio in `minute`, `hour` or `million_characters`, and explicit
Gemini cache storage in token-hours with the `hour` policy factor. Search calls,
duration audio and cache storage also receive the applicable subscription peak
factor. Its subscription-only eligibility, enabled flag, billing timezone and
left-closed/right-open window remain unchanged.
An explicitly image-scoped route with a request-priced card retains its
independent image base while selecting request-unit policy rules. A public
offer shared by text and image endpoints with different base-rate semantics
remains request-dependent instead of presenting one universal consumption rate.

For example, group base `0.8` and platform factor `1.5` produce `1.2`.
A matching model factor `2` produces `0.8 × 1.5 × 2 = 2.4`. A personal base
`0.5` with those factors produces `1.5`; a platform or model factor `0` is free.
Exact IDs take precedence over prefixes, the longest prefix wins, and a
unit-specific rule wins an otherwise equal all-unit rule. Rules are independent
of editor row order. Factors must be finite, nonnegative, no greater than
`999999.9999`, and use at most four decimal places. Invalid or unrepresentable
effective rates reject admission before upstream work instead of falling back
to a cheaper rate. Missing personal rates inherit the current group base.

Usage records preserve the accepted group revision and a public rate breakdown,
including separate platform/model factors and `rate_calculation: product_v2`.
Durable tasks accepted under the previous override calculation retain
`override_v1`: an old version-1 snapshot without a calculation field keeps its
original charge, and re-export preserves that discriminator. Unknown calculation
versions fail instead of reinterpreting accepted evidence.
Compound token/search/cache-storage charges retain separate `components` for
their distinct meters. For ordinary inference, media and cache requests,
supplier account cost multipliers stay separate from customer selling rates.
The existing Gemini balance-only batch API retains its separate historical
formula, including the selected account multiplier, batch discount and hold
margin; it is not enabled for Composite or subscription groups. Its submitted
unit-price snapshot remains authoritative. See the
[batch API contract](ASYNC_IMAGE_TASKS.md#enablement-and-configuration).
These figures do not establish an upstream's verified cost or guarantee
subscription profitability.

API Key group selectors use the authorized `view=key_quotes` catalog, independent
of the optional Available Channels page switch. Its
`rate_summary`: concrete supplier and billing-unit entries with an M=1 default
quote, model rate range, model count and exception indicator. Only configured
public offers contribute; a rate rule does not advertise a new capability.
Unknown personal rates suppress all model and summary quotes. Empty catalogs
are explicitly empty. Operation-specific base differences can leave the default
quote unknown while individual model quotes remain available.
See [Key rate disclosure](AVAILABLE_CHANNELS.md#api-key-group-rate-disclosure)
for the four selection paths and refresh/error behavior, and
[group model-factor editing](CHANNEL_PRICING.md#group-model-selling-factor-editing)
for empty, zero, deletion and shared-channel ownership semantics.

The authenticated available-model catalog and sale-scoped payment catalog share
one read-only configuration and price assembler. Finite public models come from
channel mappings/prices, group prices, account exact model mappings and enabled
exact Composite routes. Prefix routes apply to known finite IDs and are never
advertised as literal models. The group public-model allowlist filters all
sources. Concrete supplier identity is retained when an OpenAI-branded model is
served by OpenCode or another supplier. Endpoint-dependent upstream targets
without a single safe quote are marked request-dependent.

Payment reads expose only for-sale plans backed by active subscription groups.
The logged-in buyer's personal group rates apply to these consumption quotes,
including a personal rate of zero; independent media rates retain priority.
A personal-rate dependency failure returns an error instead of a group-price
quote that the buyer would not actually pay.
New request and WebSocket-turn admissions read the personal override directly
from its indexed repository row, matching the sale catalog without waiting for
process-local TTL caches. An admitted request retains its captured rate even
when a later admin edit changes subsequent requests.
They do not require the prospective buyer to own the subscription and do not
broaden the user's authorized-model endpoint. Returned model/rate summaries
contain no accounts, credentials, upstream URLs or private mapping targets.
An empty configured directory has an empty model array and null multiplier
range; dependency errors are returned as errors, not a fabricated rate of 1.
Configured models are not a promise of live provider health or remaining quota.

`model_catalog` includes `policy_version`, server `as_of`, concrete `platforms`,
`models`, and `rate_min`/`rate_max` calculated only from published offers. Each
offer's `rate_quote` identifies its actual supplier, public model, billing unit,
base source, selected policy factor, peak factor, `peak_scope` and effective multiplier.
Each directory read pins one price-card generation for reference and charged
prices, including when a background catalog refresh finishes during the read.
`price_time_multiplier` is the server-resolved current price-card period factor.
Base `pricing` remains the Standard service/standard-period reference; its
current unit estimate is base price × effective multiplier × price time factor.
Clients do no platform/model/rate selection of their own.

Purchases grant access and quota to the group. Consumption uses the current
group policy rather than freezing a historical rate at purchase time. Checkout
refreshes its sale catalog before submitting; policy version checks and order
audit metadata preserve the reviewed policy without promising a lifetime rate.
The expected policy version identifies the group's billing rules. Personal-rate
changes and future requests retain their current-rate behavior; that group
version is not a version of a buyer's personal rate or a lifetime price lock.

An admitted request freezes the group/personal rate, policy version, numeric
peak factor, its scope, pricing time and applicable price cards across same-group
account retries and detached settlement. Restoring a task after a restart or
time-zone change does not recompute that captured peak factor. New admissions
persist `peak_scope: all_meters`; older accepted snapshots without this field
restore as `token_only`, including historical `product_v2` snapshots. Re-export
preserves that scope, so delayed settlement does not reprice accepted tasks.
Re-export also retains the accepted numeric DeepSeek price-card peak factor
instead of evaluating it again with the current compiled schedule.
The private durable pricing format is version 2 for new admissions. It preserves
explicit public-model price selection and freezes the routing model used as the
input to channel mapping in `routing_model`. A detached task or restored task
uses that accepted routing model even if the current request carries a different
route; re-export preserves it. Restored version-1 tasks retain their original
price-candidate order and re-export as version 1. Tasks predating all snapshots
also keep the legacy selection and token-only peak scope. This format version is
independent of the rate-calculation and peak-scope discriminators. These are
server-owned settlement records, not fields controllable through a user's API key.
The configured Antigravity prompt-too-long fallback is a new admission to its
Anthropic fallback group: it captures that group's current personal rate,
policy and price cards, replaces the balance reservation, and removes the
original Composite supplier and alias identity before selecting an account.
An unavailable fallback quote prevents fallback selection and upstream writes;
the original admitted snapshot remains unchanged.
Grok video and Ark/Seedance tasks persist that create-time snapshot for later
status/content billing, preserving the actual supplier even though they share
the lifecycle store. Seedance retains its output-token meter.
If the pending record is missing, completion logs the missing evidence and
skips charging without consuming its settlement claim, so recovered storage can
retry. A status duration cannot replace missing policy, price or resolution.
Task status/content retrieval and Seedance deletion audit the new ingress and
validate the original key/group ownership before loading its stored prices.
They do not capture today's quote: a later invalid multiplier or personal-rate
repository outage cannot block retrieval of an accepted task. Missing or corrupt
price evidence still permits the owned lookup, while completion skips charging
and leaves the settlement claim available for repair.
Batch image jobs retain their submitted unit prices and resolved public-model
policy through balance holds, worker recovery and settlement; their usage rate
also includes the separately recorded batch discount. Existing tasks without
policy evidence retain version 0 and their previous settlement-time base,
personal, media and price-card behavior, without applying a newly introduced
platform/model policy. Their usage evidence is marked
`pricing_state: legacy_current_prices`; their original creation-time prices
cannot be reconstructed. Only tasks carrying the new snapshot freeze their
creation-time prices. Live currently writes a zero-cost
session summary: its stored policy is marked `non_billable_live` and does not
claim token or duration charging.

## User concurrency across suppliers and agent requests

The [gateway admission contract](GATEWAY_ADMISSION.md) defines the independent
user-global and `(user, group)` RPM ceilings, request/fallback/WS-turn counting,
and their existing simple-mode and dependency-failure policies. API-key USD
windows are monetary limits; they do not add an API-key RPM policy.

A user's concurrency limit is global across all API keys, groups, suppliers and
request origins. Main-agent and sub-agent requests share the same Redis user
pool; client headers and session affinity cannot create extra capacity.
Requests consult the current server-side user limit before admission, so stale
API-key or WebSocket authentication snapshots cannot enlarge the cap. A failed
limit read or Redis acquisition fails admission rather than granting capacity.
Every authoritative user-limit lookup, including direct Live callers, has a
two-second budget and respects a shorter caller deadline.
The production Redis client also preserves caller deadlines for socket reads
and writes, including TLS configurations. A longer deployment read/write
timeout cannot extend a shorter admission, release or renewal deadline.
A nonpositive user limit remains unlimited, while its executing requests stay
counted so lowering the limit sees existing work. Lowering a cap blocks new
admissions until the active count drains; it does not abort existing requests.
External HTTP requests at the cap receive HTTP 429 immediately after the ingress
audit, without entering a user wait queue, acquiring account slots, reserving
balance, or sending another provider request. This includes control endpoints,
Live creation, and independent parent/sub-agent requests. An already-upgraded
Responses WebSocket rejects a full-cap turn immediately with its retry-later
close code. Account-capacity queues remain separate. An accepted background
image worker may wait for its submitting request to hand over user capacity;
that durable worker is not another queued external request.
Every scheduler account wait uses its `WaitPlan.MaxWaiting` bound across users
and instances. For HTTP admission, overflow returns HTTP 429 immediately and a
queue-counter dependency failure returns HTTP 503 instead of creating uncounted
waiters. Queue ownership ends
when a slot is acquired, the wait times out, or the caller cancels. This applies
to generic Chat, Responses, System One, Gemini, OpenAI HTTP/WS, and native search.

Each admitted execution retains its user slot across account failover. Regular
request slots renew under the original owner, as do the existing Live leases.
API-key activity statistics renew for the same execution lifetime, including
provider draining after client disconnect. Their renewal never recreates a lost
owner, and a statistics failure does not cancel an admitted request.
A missing/expired owner cannot be resurrected by renewal; losing the lease or
failing renewal cancels that execution before capacity can be reused. Client disconnects preserve capacity while an accepted provider call drains
usage; that detached call still stops on lease loss. Request
completion releases its own slots once. Rolling startup only reaps expired
owners and preserves active requests and queue counts belonging to other
instances. Abandoned queue counters have a bounded TTL: the maximum of the
configured sticky wait, fallback wait and the worker's 30-second wait, rounded
up to whole seconds, plus 10 seconds for bounded acquisition and cleanup.
This prevents a still-active queue from expiring early or a late decrement from
consuming a newly admitted queue entry.

An acquisition whose Redis reply is lost can already have written its owner.
That uncertain owner is deleted by its exact request ID. A failed delete or
release retains a bounded recovery record; the next admission for that user or
account retries its pending owners before acquiring capacity. Persistent failure
returns dependency-unavailable, rather than treating an unconfirmed count as
free capacity. The existing slot-cleanup worker also processes at most 64
pending owners per batch. Neither path deletes an entire user/account key or
another execution's owner. Each instance reserves recovery space before the
Redis write, with a maximum of 65,536 active/pending owner records; exhaustion
rejects new writes with HTTP 503. Confirmed release or the completed owner's
conservative Redis lifetime frees that space. Active executions do not expire
from this local registry while they still own execution capacity.
When periodic cleanup is disabled, a full registry also reclaims proven-expired,
non-busy pending records before refusing an unrelated user's admission.

HTTP handlers install the shared execution context before acquiring capacity.
`AcquireResult.Context` is the context the upstream operation must use. A
normal account release does not cancel it, allowing failover under the same
user slot. Detached workers remove the submitting request's capacity domain
with `WithoutConcurrencyLeaseContext`, then create a new domain and acquire
capacity for their execution. Context metadata, including accepted prices,
remains available. Async image workers retain one user slot through provider
execution, result download, object upload, and terminal task persistence. Their
synchronous gateway call reuses a private, revocable delegation of that same
owner; client headers, another user, and separately detached workers cannot
borrow it. Lease loss also cancels actual storage/download HTTP operations.
Their API-key activity uses the same private worker ownership and remains one
through result storage and terminal persistence. Batch-provider workers track
their submitting key for each execution, releasing statistics while requeued.
Idle WebSocket ingress leases retain their separate
connection guard; each executing turn enters the shared user pool.

Live client and observer controllers treat a missing lease or Redis renewal
error as terminal. Before releasing the session, they send the existing
`session.close` control message with a bounded timeout and close the sideband
socket even if that write fails. They do not reconnect a session whose execution
capacity can no longer be confirmed. This behavior is verified against isolated
real WebSocket responders; it does not establish authorization or media-session
behavior for a real upstream account.

Live creation requires the selected credential owner's configured WS control
transport to be enabled. A global, deployment, account-type or account-level
restriction rejects that account before creating a provider session.

If Live creation succeeds but saving its mapping fails, the gateway closes that
exact upstream call using the original credential owner's captured identity,
attestation and proxy. It does not replay creation or start a normal observer.
Each close attempt has a two-second budget independent of caller cancellation;
confirmed closure releases only this call's lease. An unconfirmed close retains
the lease and enters paced compensation, bounded by the earlier of the session
expiry and one hour. The gateway attempts to renew the original lease while
retrying; continuous Redis failure can still let that lease expire. Reaching
the compensation deadline releases local capacity and logs that remote closure
was not confirmed. It does not establish that an unreachable provider stopped
the call. At most 128 Live creations or orphan cleanups may be outstanding per
instance: a reservation is required before account selection or upstream
creation, returned after successful persistence, creation failure, or cleanup
completion. A full reservation pool rejects with HTTP 503 before upstream work.
An upstream success response also prevents creation replay when reading its SDP
answer fails. A known call ID follows the same original-owner compensation.
If a success response has no usable call location, the gateway cannot confirm
remote termination: it keeps the original lease and recovery reservation for
the same bounded lifetime, attempts lease renewal, and emits structured failure
reasons without repeatedly dialing an unknown call. At expiry it releases local
resources and reports unconfirmed closure. It does not invent a provider call ID.
Live mapping persistence and cross-instance controller handoff preserve the
create-time pricing evidence used by the zero-cost session summary.
These internal recovery bounds add no user-visible RPM or concurrency setting.

The core lease and fresh-limit contracts are exercised by
`backend/internal/service/concurrency_lease_test.go`; real Redis owner isolation,
renewal past expiry, unlimited-to-limited changes and multi-instance parent/child
capacity are exercised by
`backend/internal/repository/concurrency_global_lease_integration_test.go`.
Uncertain acquisition replies, transient release failures, bounded recovery and
cleanup-disabled expiry are covered by
`backend/internal/service/concurrency_owner_recovery_test.go` and the real Redis
`backend/internal/repository/concurrency_owner_recovery_integration_test.go`.
`backend/internal/repository/redis_deadline_test.go` checks slow reads, blocked
writes and concurrent capacity operations over real TCP with the production
Redis client.
Live persistence failure, original-owner close confirmation, recovery capacity
and expiry are covered by
`backend/internal/service/openai_live_create_business_test.go` and
`backend/internal/service/openai_live_create_incomplete_test.go`. Live mapping
policy preservation is covered by `gateway_cache_live_test.go` and
`TestGatewayCacheLiveCallSnapshotRealRedis` in the repository package.
Mixed HTTP, active Responses WebSocket and Grok Voice capacity, with Live rejected
at the same user cap, is exercised by
`backend/internal/handler/gateway_mixed_protocol_concurrency_test.go`. The two
Live controller renewal-failure paths and actual close frames/socket termination
are exercised by
`backend/internal/service/openai_live_lease_loss_wire_test.go`.

## Admin Workflows

- Group create/update accepts `billing_rate_policy`; omission on update preserves
  it, while an explicit empty object clears it. Invalid rules return HTTP 400
  `INVALID_BILLING_RATE_POLICY` without persisting the edit. At most 256 model
  rules are allowed. Group responses include `billing_policy_version`.
- The editor submits its opening revision as `expected_billing_policy_version`.
  A stale revision or concurrent persisted group writer receives HTTP 409
  `GROUP_CONFIGURATION_CHANGED`; the editor keeps its draft for review and
  reload. The repository compares the revision atomically, including writes
  that do not change selling rates. Batch sort changes and startup configuration
  backfills also advance the revision, so an already open editor must reload.
- Cancelling subscription confirmation or leaving checkout invalidates its
  pending quote refresh. Selecting the same plan again requires a new
  confirmation; an earlier response cannot create an order or report a stale
  refresh error.
- A new request whose effective rate or pricing dependency is unavailable
  receives HTTP 503 `BILLING_RATE_UNAVAILABLE` after ingress audit and before
  capacity/account admission. This is a billing failure, separate from content
  policy decisions.
- Admins can create a group with platform `composite`.
- Admins can add, edit, delete, and preview composite model routes.
- Composite groups can copy accounts from concrete provider groups.
- Concrete provider accounts can be assigned directly to composite groups from
  account create/edit and bulk account workflows.
- Subscription payment plans can bind to a composite group when that group's
  `subscription_type` is `subscription`. The plan grants access to the
  composite group; each request is still billed and quota-checked against the
  resolved concrete provider platform.
- Channel configuration exposes composite groups in concrete provider sections.
  The channel `group_ids` payload is still flat; provider-specific model
  mapping and pricing remain keyed by concrete platform.

## Bucket 2 Setup: OpenAI + Claude + Gemini + Grok

Use one composite subscription group when one customer-facing plan should expose
model aliases across OpenAI, Claude, Gemini, and Grok without issuing separate
keys per provider.

1. Create concrete provider groups for the upstream account pools, for example
   `OpenAI Paid`, `Claude Paid`, `Gemini Paid`, and `Grok Paid`.
2. Create a `composite` group with `subscription_type` set to `subscription`.
3. Assign provider accounts directly to the composite group, or copy accounts
   from the concrete provider groups during group creation.
4. Add explicit routes for public aliases that should not rely on built-in
   model detection:

   | Public model | Endpoint | Target platform | Upstream model |
   | --- | --- | --- | --- |
   | `all/gpt-5` | `responses` | `openai` | `gpt-5` |
   | `all/claude-sonnet` | `messages` | `anthropic` | `claude-sonnet-4-6` |
   | `all/gemini-pro` | `gemini` | `gemini` | `gemini-2.5-pro` |
   | `all/grok` | `responses` | `grok` | `grok-4.3` |

5. Configure channel pricing and model mapping under the concrete platforms
   named in each route. Composite routing does not create pricing records.
6. Create a subscription payment plan for the composite group.

The same composite group can also rely on built-in detection for standard model
names such as `gpt-*`, `claude-*`, `gemini-*`, and `grok-*`. Explicit routes are
recommended for bundled plan aliases because they make endpoint, provider, and
upstream model attribution reviewable in the admin UI.

## Limits

Composite routes choose a concrete provider and upstream model; they do not
create synthetic model metadata, pricing, or upstream capability records by
themselves. Keep channel pricing/model mapping configured for the concrete
provider platforms that the routes target.

Composite groups do not implement:

- AUTO smart-routing among multiple providers for the same abstract task.
- Direct API-key binding to several existing groups without a composite group.
- Protocol-agnostic provider decoupling or a LiteLLM-style adapter rewrite.

## Billing regression ownership

These are acceptance-test owners, not a claim that every possible combination
has been executed. Run checks in the repository validation container.

| Contract | Regression source |
| --- | --- |
| Model/platform precedence, inheritance, zero, units and invalid rules | `backend/internal/domain/billing_rate_policy_test.go` |
| Immediate user rejection across keys, suppliers, HTTP, WS, Live and image pool contention | `backend/internal/server/routes/gateway_global_concurrency_test.go`, `backend/internal/handler/gateway_mixed_protocol_concurrency_test.go`, `backend/internal/handler/openai_ws_global_concurrency_test.go`, `backend/internal/handler/image_user_capacity_admission_test.go` |
| Literal request charges ($12 and peak $18), defaults and all registered suppliers | `backend/internal/service/composite_product_billing_acceptance_test.go` |
| Real HTTP routing, account/channel mappings, wire credentials, ledger charges and released slots | `backend/internal/server/routes/composite_billing_acceptance_test.go` |
| Public price ownership, frozen routing and historical durable selection | `backend/internal/service/public_model_pricing_acceptance_test.go` |
| Codex terminal-model observations cannot replace explicit paid/free public cards; explicit sources and legacy correction remain | `backend/internal/service/codex_public_price_correction_acceptance_test.go` |
| Inflight per-request/media effort factors, endpoint metadata and foreign-field isolation | `backend/internal/service/inflight_request_effort_acceptance_test.go`, `backend/internal/handler/gateway_inflight_reservation_test.go`, `backend/internal/handler/inflight_pricing_protocol_test.go` |
| Selected-account price reconciliation, Luna mappings, public cards/zero, source precedence and final settlement | `backend/internal/service/inflight_account_mapping_acceptance_test.go`, `backend/internal/service/inflight_selected_contract_test.go` |
| Final mapped-price rejection before HTTP forwarding, own-resource cleanup and preservation of other owners | `backend/internal/handler/gateway_account_inflight_boundary_test.go`, `backend/internal/handler/openai_account_inflight_boundary_test.go`, `backend/internal/handler/gateway_inflight_account_plan_test.go`, `backend/internal/handler/gateway_account_inflight_retail_success_test.go` |
| Independent WS turn holds, asynchronous billing handoff, zero-dial rejection and retry pricing ownership | `backend/internal/handler/openai_ws_inflight_reservation_test.go` |
| Atomic hold increases, live-owner fencing and cross-instance Redis contention | `backend/internal/repository/billing_inflight_update_test.go`, `backend/internal/repository/billing_inflight_selected_owner_integration_test.go`, `backend/internal/service/billing_inflight_increase_lifecycle_test.go` |
| Mapped and converted effort/defaults on the public card, with actual wire and settlement assertions | `backend/internal/service/inflight_mapped_effort_acceptance_test.go`, `backend/internal/service/inflight_converted_effort_acceptance_test.go` |
| Raw Chat streaming settles the forwarded GLM/Grok effort, preserves ordinary effort and explicit free public cards | `backend/internal/service/inflight_raw_stream_effort_acceptance_test.go` |
| Fast policy snapshots, Messages conversions, exact beta declarations, normalized model matching and native-provider isolation | `backend/internal/service/inflight_openai_fast_policy_acceptance_test.go`, `backend/internal/service/inflight_openai_fast_conversion_acceptance_test.go`, `backend/internal/handler/inflight_anthropic_fast_test.go` |
| One account-pool read per estimate, zero reads for ordinary/group-only Fast with captured protocol metadata, shared deadline across pools, parent isolation and fresh reads after recovery | `backend/internal/service/inflight_fast_effort_lookup_test.go`, `backend/internal/service/inflight_snapshot_lookup_test.go` |
| Queue-expired late successes cannot forward or remove another owner; accepted executions outlive queue cancellation and release once | `backend/internal/handler/gateway_wait_deadline_test.go`, `backend/internal/handler/gateway_helper_lease_test.go` |
| Empty/partial price inheritance, explicit zero and unavailable pricing | `backend/internal/service/model_pricing_inheritance_acceptance_test.go` |
| Actual token dimensions, input-only embeddings, null versus zero and restored partial cards | `backend/internal/service/partial_token_pricing_acceptance_test.go` |
| Customer charges, subscription quota, separate supplier cost and accepted pricing | `backend/internal/service/billing_rate_policy_test.go` |
| Fresh personal overrides and preservation of in-flight prices | `backend/internal/service/user_group_rate_resolver_test.go` |
| Supplier/public-model catalog isolation and safe conditional prices | `backend/internal/service/commercial_catalog_test.go` |
| Audit before pricing failure, with no capacity or account admission | `backend/internal/handler/billing_pricing_boundary_test.go` |
| Native Seedance supplier admission and preservation of native model fields | `backend/internal/handler/seedance_test.go`, `backend/internal/server/routes/composite_platform_test.go` |
| Explicit fallback-group pricing, supplier identity and reservation replacement | `backend/internal/handler/gateway_prompt_fallback_pricing_test.go` |
| Task lookup after price changes/outages and ownership before importing evidence | `backend/internal/handler/grok_media_lookup_pricing_test.go` |
| Deferred task recovery, missing price evidence, legacy tasks and no duplicate claims | `backend/internal/handler/seedance_test.go`, `backend/internal/handler/grok_media_slots_test.go` |
| Batch holds/recovery and zero-cost Live evidence | `backend/internal/service/durable_billing_policy_test.go` |
| Provider-complete Batch work releases capacity before index/settlement retries or output-reference persistence | `backend/internal/service/batch_image_unfinished_lifecycle_test.go` |
| Media billing timeout/panic/process-loss recovery, frozen quotes, stable task identity across changed asset IDs and owner CAS | `backend/internal/handler/media_billing_claim_recovery_test.go`, `backend/internal/repository/gateway_cache_media_billing_test.go` |
| Upgrade capacity repair and uncertain binding; real PostgreSQL/Redis settlement deduplication across instances | `backend/internal/repository/gateway_unfinished_tasks_integration_test.go`, `backend/internal/repository/gateway_cache_media_billing_integration_test.go` (integration) |
| Composite Gemini cache ownership and actual lifecycle charges | `backend/internal/handler/gemini_cached_contents_composite_test.go` |
| Migration defaults, optimistic writes and usage-evidence SQL round trips | `backend/internal/repository/*billing*integration_test.go` |
| Sale API peak/off-peak boundaries, personal base, model defaults and public projection | `backend/internal/handler/payment_model_catalog_test.go` |
| Checkout stale-policy/plan protection and persisted evidence | `backend/internal/service/payment_order_billing_policy_test.go` |
| Key create/edit and sale cards expand same-name suppliers separately; new and historical peak quotes | `frontend/src/views/user/__tests__/KeysView.spec.ts`, `frontend/src/components/payment/__tests__/PlanModelCatalog.spec.ts`, `frontend/src/components/payment/__tests__/SubscriptionPlanCard.spec.ts` |
| Admin draft/zero/unknown quotes and checkout reconfirmation | Frontend billing-rate, catalog, payment and group-editor specs |
| Partial token prices preserve known dimensions and explicit zero; media/search previews are base references in both locales | `frontend/src/components/channels/__tests__/catalog.spec.ts`, `frontend/src/views/admin/__tests__/GroupsView.duplicate.spec.ts`, `frontend/src/i18n/__tests__/opsLocaleKeys.spec.ts` |
| Real browser persistence and stale-editor conflict | `composite billing` scenario in `frontend/e2e/seven-provider-management.spec.ts` |
