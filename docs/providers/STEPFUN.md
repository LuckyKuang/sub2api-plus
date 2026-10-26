# StepFun / Step-Code

StepFun is a separate account and group platform (`stepfun`). It supports API
keys and browser-acquired Step Plan credentials (`oauth`). Both use Bearer
authentication and the shared Step-Code [outbound identity](../OUTBOUND_IDENTITY.md).

| Region | API Key pay-as-you-go base | Step Plan base |
| --- | --- | --- |
| China (`cn`, default) | `https://api.stepfun.com/v1` | `https://api.stepfun.com/step_plan/v1` |
| International (`global`) | `https://api.stepfun.ai/v1` | `https://api.stepfun.ai/step_plan/v1` |

The native protocol is Chat Completions (`POST <base>/chat/completions`).
Responses and Messages clients use the existing audited conversion paths.
This is the executed Step-Code provider contract: both
[`features/step-provider/index.ts`](https://github.com/stepfun-ai/Step-Code/blob/81e79bc4b699148d6823fedde9b5e70d9a80255f/packages/coding-agent/src/features/step-provider/index.ts)
and [`step-provider/index.ts`](https://github.com/stepfun-ai/Step-Code/blob/81e79bc4b699148d6823fedde9b5e70d9a80255f/packages/providers/src/step-provider/index.ts)
select `openai-completions`, and
[`api/openai-completions.ts`](https://github.com/stepfun-ai/Step-Code/blob/81e79bc4b699148d6823fedde9b5e70d9a80255f/packages/providers/src/api/openai-completions.ts)
sends `client.chat.completions.create`. Older `anthropic-messages` prose in the
official integration guide does not describe this execution path.
No native Responses, Messages or Responses WebSocket support is inferred from
the general SDK adapters. API keys can use an administrator-configured proxy
base; native OAuth credentials remain pinned to their selected official region.
Explicit deployment upstream allowlists must include the selected API hostname.

The supported profile matrix has six rows: OAuth Plan CN/global, API Key Plan
CN/global and API Key pay-as-you-go CN/global. OAuth login always yields a Plan
profile. Each row independently accepts native Chat Completions and the HTTP
Responses/Messages conversions, with streaming and buffered responses; the
conversion entries are separate gateway contracts and not upstream-native
Step APIs. A particular model's region, Plan entitlement and discovered
capabilities still depend on the actual account.

For tool streams, distinct Chat tool indexes retain their own call IDs, names
and argument fragments. The Messages bridge emits sequential content blocks:
the first announced tool keeps streaming; other concurrent tools accumulate
their own fragments until their blocks can open. Each block closes after its
arguments, and no later delta targets a closed block. Subsequent client turns
retain both assistant calls and their corresponding tool results.
Text, refusal and reasoning deltas interleaved with an open tool's arguments
wait until the sequential tool blocks finish. Tool input must be a complete
JSON object before the Messages bridge emits a completed tool-use terminal event;
empty or whitespace-only arguments retain the empty-object convention. A
damaged, scalar, array or null tool input returns a safe upstream error with
observed partial usage, rather than a successful empty body or terminal event.
The normal `length` stop instead retains Messages `stop_reason=max_tokens` and
the actual reported usage; it does not count as a provider failure. A streaming
response preserves the original partial tool-argument deltas. A buffered
response retains each tool ID/name and complete object input, but represents
incomplete or non-object input as `{}` so the response remains valid JSON.
Clients should retry with more output budget after `max_tokens` rather than
execute that placeholder. This is the gateway's Chat-to-Messages adaptation
contract, consistent with the upstream Messages distinction between
[token-limit stops and errors](https://platform.claude.com/docs/en/build-with-claude/handling-stop-reasons#max_tokens).
The Responses conversion preserves `length` as `status=incomplete` with
`incomplete_details.reason=max_output_tokens`; its streaming terminal event is
`response.incomplete`, as specified by the
[Responses streaming contract](https://developers.openai.com/api/reference/resources/responses/streaming-events).
Function-call arguments remain their original strings, including truncated
input, and truncated calls have `status=incomplete`. Completed turns with
invalid ordinary function arguments return a safe error with observed partial
usage, instead of silently dropping the tool or reporting exact completed usage.
Custom and search tools retain their separate argument contracts.

An explicit upstream JSON/SSE `error` object remains an inference failure even
under HTTP 200. An error before client output may use the established account
failover path; an error after committed output returns an incomplete result
without replaying that turn. HTTP 401/403/429/5xx retain the gateway's existing
status policies. A late failure cannot quarantine a replacement key, profile or
proxy binding; the unchanged failed owner remains eligible for error handling.
Each physical attempt contributes one scheduling outcome. A partial failed
response may retain one usage record, but cannot count twice toward account
health or evict the conversation owner merely because billing continues.

The raw Chat stream accepts `[DONE]`, a finish reason, or an explicit usage
frame as its existing completion evidence. The Responses and Messages bridges
require `[DONE]`; a missing sentinel or read failure returns incomplete usage
before synthetic success-finalization. A caller cancellation preserves already
observed tokens and does not authorize another inference send. Partial results
are marked incomplete. Successful completion without reported usage retains
zero observed tokens and cost with `usage_source=unknown`; response completion
does not establish exact metering. Actual reported usage remains
`upstream_exact` for complete responses and `partial` for incomplete responses.
The gateway does not invent precise Step token counts.

## Browser login

Accounts → StepFun offers Step Plan browser login and manual API Key entry.
The server starts `POST /api/stdhttp/v1/cli-login/init` on
`https://platform.stepfun.com` (international: `https://platform.stepfun.ai`).
The browser opens `/cli-login-remote?flow_id=…`; the panel automatically polls
the server, which sends `GET /api/stdhttp/v1/cli-login/poll/{flow_id}`.
The public flow ID appears in that browser URL. The separate polling credential
is 32 random bytes encoded as 64 hexadecimal characters. It stays server-side and appears only in the upstream Authorization
header. The retired loopback callback is not an alternate credential source.

Initialization declares the regional profile (`step_plan` or
`step_plan_oversea`), matching origin and client metadata
`{name:"stepcode", version:"0.84.4", platform:"linux-x64"}`. That version is
login audit metadata; the Step-Code product User-Agent remains explicitly
versionless. Both control calls use the trusted Step-Code identity and selected
proxy, disable redirects and bound response size. Browser navigation keeps the
browser's own headers.

Sessions bind the initiating administrator, platform, region, proxy and identity.

The selected proxy must be a complete, valid captured binding before login,
Passport refresh, inference, model discovery or Plan usage can run. A missing or
mismatched hydrated proxy cannot fall back to direct egress. Model preview keeps
the session's original proxy URL even if that proxy row is edited during login.
Reauthorization completion compares the captured account, proxy route and
recoverable state atomically with the credential write and cooldown reset; a
later proxy edit or manual scheduling change requires a new login.
Sessions expire after at most ten minutes or the provider's earlier deadline. Polling
honors the supplied interval, bounded to 1–60 seconds. Cancellation, rejection
and expiry stop authorization. Sessions use the shared Redis store when
configured and are claimed once before account creation; Redis failure never
falls back to a local session. Public responses and errors do not echo tokens.

A ready response contains the static model API key and may include a separate
account access/refresh session. Only the model key authenticates inference,
model discovery and Plan usage. The account session has its own fields and
expiry; its TTL never becomes the model key's expiry. An unusable optional
session does not discard an otherwise valid model key.
Relinking replaces the complete optional account session. If the new grant has
no session, all previous `step_account_*` credentials are removed; the new model
key still supports inference, discovery and Plan usage, while account-session
refresh requires a new account login.

The account refresh action can renew the optional Passport session through
`POST /passport/proto.api.passport.v1.PassportService/RefreshToken` on the stored
region's platform origin. It uses the `Oasis-Token` cookie and the session's
app/device declarations. An incomplete session or a disabled account is rejected
without an issuer request or credential write; a coordinator skip does not make
an unavailable session a successful refresh. Only signed-in `mode:2` is accepted. A missing returned
refresh token retains the previous one; transport, HTTP and unexpected-response
failures retain stored session material for a later attempt. The session is not
registered with the background model-token refresh pool. Refresh failure or an
expired account session does not replace, expire or revoke the static model key.
A revoked model key requires browser reauthorization. Relinking retains the
account's region, proxy, metadata and identity and replaces obsolete grant fields.

## Models, billing and monitoring

Model sync calls `GET <base>/models` with the same account. Only entries tagged
`大语言模型` or `路由模型` are exposed when type metadata is present; entirely
untyped catalogs retain their IDs. Context, vision, reasoning and supported
efforts use the reported metadata. No static model availability or free prices
are inferred. Account connection tests require an
explicit model from that account's catalog.

Successful StepFun sync saves every returned chat/router ID, including rows with
incomplete metadata. Missing capabilities stay unknown and are not filled from
a third-party registry. Removed IDs leave the snapshot on the next successful
sync; failed discovery preserves the previous snapshot.

Model restrictions show Step-Code's recognized native IDs immediately, including
before credentials are entered: `step-5-preview`, `step-3.7-flash`,
`step-3.5-flash-2603`, `step-3.5-flash`, and `step-router-v1`. They come from
`STEP_MODEL_IDS` in `packages/coding-agent/src/step/defaults.ts` at the reference
commit below; `step-5-preview` is that client's default. These are configuration
candidates, not an assertion of entitlement in either region or account type.
The shared [provider catalog](../CN_PROVIDER_MODELS.md) also supplies group
restriction candidates. New account forms retain the shared default-selection
behavior, and **Fill related models** (Chinese: 同步最新支持模型) remains visible
before login; it fills the maintained candidates and any models already discovered.
It does not make an unauthenticated upstream request.

To discover the account's live catalog, enter an API key or finish the Step Plan
browser authorization, then use **Sync upstream models** in Model restrictions.
The returned IDs join the searchable checkbox options and remain available after
deselection. Saved whitelist IDs also appear when editing an account. Discovered
options are scoped to the credential source and late responses from a previous
source are discarded. Manual model IDs and model mappings remain supported.
An empty restriction allows all models; a nonempty whitelist/mapping restricts
eligible request model IDs for both API Key and OAuth accounts.

Client `/v1/models` (including single-model retrieval) and Codex catalogs use
account model mappings when present. Unrestricted StepFun accounts contribute
their synced catalog, or the maintained native candidates before the first sync;
they never fall back to Claude models. Group allowlists filter these IDs as
usual. Synced vision, reasoning and context metadata also reaches Codex manifests,
including mapped public aliases. This applies to standalone and composite groups.

### Default prices

The bundled catalog and same-model billing fallback use the official international
USD prices below (checked 2026-10-08). They also appear in channel reference-price
lookup and model sync. An exact dynamic catalog entry takes precedence over the
built-in fallback; saved channel pricing takes precedence over default prices.

| Model | Input / 1M tokens (cache miss) | Cached input / 1M tokens | Output / 1M tokens |
| --- | ---: | ---: | ---: |
| `step-5-preview` | $1.00 | $0.05 | $2.70 |
| `step-3.7-flash` | $0.20 | $0.04 | $1.15 |
| `step-3.5-flash` | $0.10 | $0.02 | $0.30 |
| `step-3.5-flash-2603` | $0.10 | $0.02 | $0.30 |
| `step-1o-turbo-vision` | $0.36 | $0.07 | $1.15 |

Sources: [international USD pricing](https://platform.stepfun.ai/docs/en/guides/pricing/details),
[China CNY pricing](https://platform.stepfun.com/docs/zh/guides/pricing/details),
and [official cache semantics](https://platform.stepfun.com/docs/zh/guides/developer/prompt-cache).
Defaults are proxy billing in USD, shared by OAuth and API Key accounts. China's
CNY settlement rates and Step Plan subscription Credits are different units;
there is no automatic currency or subscription-credit conversion. Operators can
set channel prices to reflect their own settlement and resale policy.

Browser grants are saved only as Step Plan (`coding`) accounts; marking a native
OAuth grant as pay-as-you-go is rejected rather than silently using the Plan URL.

`usage.prompt_tokens` includes `usage.cached_tokens`: subtract the hits once
before applying the ordinary input price. Cache misses include cache creation;
there is no additional write surcharge. If usage separately counts cache writes,
that disjoint bucket uses the ordinary input rate. Reasoning and final-answer
tokens are both included in output. These semantics apply to streaming and
non-streaming Chat Completions and the Responses/Messages bridges.

[`step-router-v1`](https://platform.stepfun.com/docs/zh/guides/models/step-router)
is a Step Plan router that charges according to the selected `deepseek-v4-pro`
or `step-3.7-flash` engine, then consumes Plan Credits. It has no single fixed
token price. It stays visible as `manual_required`: configure a channel billing
rule before using it. Unknown IDs and unregistered date/suffix variants also
remain unpriced; they never inherit another StepFun model's rate. Dedicated
speech/image APIs and value-added charges are outside this Chat Completions
integration's default token pricing.

`POST /api/v1/admin/cn/oauth/stepfun/models` accepts only an owned, ready,
unexpired `session_id`. It reads the authorized region's Step Plan catalog
using the session's trusted identity, returns only model IDs, and creates no
account. Cancelled, consumed and pending sessions cannot query it. OAuth
completion accepts `model_mapping` for new accounts; reauthorization preserves
the existing restriction. Credentials stay on the server throughout discovery.

StepFun participates in groups, composite routing (`stepfun/…`, `step/…`,
`step-…`), quotas, statistics and probes. OAuth Plan usage queries
`GET https://api.stepfun.{com|ai}/step_plan/v1/usage` with the model key and
same-owner identity. The quota projection reads schema version 1,
`personal_account`, Coding Plan windows: `five_hour` and `weekly` map to the
existing 5h/weekly snapshot. Only explicit limited-window percentages are
observations. Missing, unlimited, malformed or unknown windows do not become
zero usage; an unrecognized response preserves the previous snapshot.
The response must satisfy the complete supported schema. A valid partial or
unlimited snapshot, including both windows being unlimited, clears old
percentages/reset timestamps for unobserved windows while advancing freshness;
unlimited never means an invented zero-percent observation. Persistence atomically
compares the captured full credential owner,
platform/type, proxy and active account row; relinking or deletion cannot save
the previous owner's quota. A failed comparison changes no snapshot, cache or
outbox, and an outbox failure rolls the observation back. Such a result is not
reported as a usable successful observation. Periodic collection includes Step
Plan OAuth accounts; manual API-key accounts have no Plan quota query.
Token-plan credit buckets and a monetary balance are not projected onto these
percentage windows. A token-plan/credit-bucket or wallet-only response is an
unsupported observation: it cannot refresh or overwrite the last authoritative
percentage snapshot. CNY, USD and Plan Credits are not converted into one
another, and do not supply an unpriced model with an inferred gateway USD rate.
Local request usage remains available independently.
Migration 277 extends platform/probe constraints.

## Remote web search MCP contract audit

Step-Code's `search_web` uses a provider-hosted remote service. This is distinct
from the agent's local MCP configuration and tool-execution loop. The gateway
does not currently expose this remote search service or claim search billing
equivalence. This implementation batch audits the contract; adding a unified
search gateway requires a separate explicit scope decision.

At the reviewed source commit, the remote tool is `stepsearch.web_search` with
arguments `{query, n:10}` and Bearer model/search-key authentication. The
official profile determines an independent search endpoint:

| Profile | Search endpoint |
| --- | --- |
| CN Plan | `https://api.stepfun.com/step_plan/v1/mcp/web_search/mcp` |
| Global Plan | `https://api.stepfun.ai/step_plan/v1/mcp/web_search/mcp` |
| CN pay-as-you-go | `https://api.stepfun.com/v1/mcp/web_search/mcp` |
| Global pay-as-you-go | `https://api.stepfun.ai/v1/mcp/web_search/mcp` |

An explicit search URL precedes `STEPCODE_SEARCH_WEB_MCP_URL`, then the saved
profile endpoint. An origin-only override retains that profile's path. Model
base-URL overrides do not redirect search. The key precedence is explicit tool
key / CLI `--api-key`, then `STEPCODE_SEARCH_API_KEY`, then saved Step login
credentials. `STEP_API_KEY` is intentionally excluded because it may contain a
gateway model key that cannot be sent to the fixed Step search service.

The official client pins MCP SDK `1.27.1` and uses Streamable HTTP with client
name `stepcode` and the declared Step-Code version. It connects/initializes,
collects the complete paginated `tools/list` snapshot, finds the declared tool
and sends `tools/call`. A repeated pagination cursor fails. Calls bind output
schema validation to the discovered tool generation; catalog changes cannot
change an in-flight call's validator. Tools requiring task execution fail
explicitly. The caller's cancellation signal is combined with a default
30-second deadline; abort closes the transport even while initialization is
pending, and completion closes the client. Tool errors and invalid structured
output remain failures.

These contracts come from
[`search-web-tool.ts`](https://github.com/stepfun-ai/Step-Code/blob/81e79bc4b699148d6823fedde9b5e70d9a80255f/packages/coding-agent/src/step/search-web-tool.ts),
[`mcp-client.ts`](https://github.com/stepfun-ai/Step-Code/blob/81e79bc4b699148d6823fedde9b5e70d9a80255f/packages/coding-agent/src/step/mcp-client.ts)
and the pinned
[`package.json`](https://github.com/stepfun-ai/Step-Code/blob/81e79bc4b699148d6823fedde9b5e70d9a80255f/packages/coding-agent/package.json).
Source review does not verify live search prices, regional authorization or
every server session/protocol version. Future gateway implementation must
freeze those contracts, search-specific charges, session ownership, cancellation,
proxy failures and credential isolation before declaring support. Chat token
prices cannot serve as search prices.

## Source and regression evidence

The [official Step-Code source](../OFFICIAL_AGENT_BASELINES.md) at commit
`81e79bc4b699148d6823fedde9b5e70d9a80255f` (2026-10-25 review) uses:

- `packages/providers/src/step-provider/login-client.ts` and `login-refresh.ts`:
  cloud polling, static model key, optional account session and Passport refresh.
- `packages/providers/src/step-provider/index.ts`: catalog and native protocol.
- `packages/coding-agent/src/step/usage/account.ts` and `types.ts`:
  Plan usage endpoint, schema and supported window semantics.
- `packages/coding-agent/src/step/onboarding.ts`: regional endpoint profiles.
- `packages/coding-agent/src/step/defaults.ts`: recognized native model IDs used
  as management candidates; the provider's live catalog remains authoritative.
- `packages/providers/src/api/openai-completions.ts` and
  `utils/pi-user-agent.ts`: actual model UA and SDK selection.
- `packages/coding-agent/src/step/environment.ts`, `features/step.ts`, and
  `step/trace-headers.ts`: `x-step-client` and optional trace gating.
- OpenAI JS 6.40.0 `src/internal/detect-platform.ts`: SDK wire declarations.

The published OpenAI JS 6.40.0 package was also exercised in the validation
container with the pinned Linux/x64/Node runtime fixture; its captured request
matched all eight documented inference identity headers. No live credential
was used for that SDK wire check.

Requirement-based tests use literal official URLs/headers, adversarial
cloud-login responses, polling state changes, secret isolation, cancellation
and account-session separation. Transport tests exercise model forwarding,
probes and discovery
for both account types and regions. Real-provider authorization still requires
an operator's account; mock wire tests do not assert live authorization success.

The capability-completion regressions are maintained separately from claims of
live provider acceptance:

| Contract | Regression entrypoint |
| --- | --- |
| Six profiles × three HTTP entries × streaming/buffered, reasoning/text/cache and independent USD charges | `service/TestStepFunCapabilityBasicMatrix` |
| Independent model prices, explicit channel overrides, unknown/router manual pricing, and unsupported credit/wallet observations | `service/TestStepFunPricingOfficialRatesAndReferenceCoverage`; `service/TestStepFunPricingCatalogAndChannelOverrides`; `service/TestStepFunOfficialCachedUsageAcrossProtocols`; `service/TestStepFunCreditsAndWalletsNeverBecomePercentageQuota` |
| Two interleaved tools, complete arguments and actual second-turn Chat history | `service/TestStepFunCapabilityParallelToolsAndResults`; `apicompat/TestStepFunParallelChatToolsKeepIndependentAnthropicBlockLifecycles` |
| Rejections, explicit HTTP 200/SSE errors, EOF/read failures, missing usage/sentinel and caller cancellation | `service/TestStepFunCapabilityFailuresNeverBecomeSuccess`; `service/TestStepFunCapabilityStreamFailureAndTerminationContract`; `service/TestStepFunCapabilityCompletedWithoutUsageKeepsUnknownBillingSource` |
| In-flight key/profile/proxy replacement, including same-row proxy edits and unchanged-owner control | `service/TestStepFunCapabilityLate401CannotQuarantineReplacementOwner` |
| Actual production HTTP client, timeout/refusal/407, zero direct-connection attempts and sanitized failures | `repository/TestStepFunProductionProxyFailureAcrossProfilesAndBridges` |
| Full six-profile/three-entry/streaming matrix over verified TLS and authenticated proxies, 503/429 A→B, restored A losing to B on another gateway, real Redis leases/affinity and PostgreSQL charges/dedup | `integration/TestStepFunBusinessMatrix` |

The central [seven-provider matrix](../SEVEN_PROVIDER_TEST_MATRIX.md) records
which commands actually ran on a given tree. Listed scenarios are executable
contracts, not a claim that every test or live-account combination has run.

## Native client selection

OAuth and API-key accounts retain the official `stepfun` client family. The
versionless product identity and inference SDK fingerprint stay fixed. Neither
account selections nor a `stepfun:apikey` default mapping can switch to another
client. The settings page retains the Step-Code identity preview, with compatible
supplier mappings confined to their own advanced section. Migration 278 removes
stale cross-family selections without changing credentials, models or pricing.
