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
No native Responses, Messages or Responses WebSocket support is inferred from
the general SDK adapters. API keys can use an administrator-configured proxy
base; native OAuth credentials remain pinned to their selected official region.
Explicit deployment upstream allowlists must include the selected API hostname.

## Browser login

Accounts → StepFun offers Step Plan browser login and manual API Key entry.
The browser URL is `https://platform.stepfun.com/cli-login?port=53683&state=…`
(international: `platform.stepfun.ai`). After authorization, paste the complete
`http://127.0.0.1:53683/callback?...` URL into the login panel, even if that
loopback page cannot load. The remote server does not bind a callback listener
on the administrator's computer. Treat that URL as a secret.

The panel sends it in an authenticated POST body. Backend validation requires
the exact loopback destination, one matching state, one credential, the
initiating administrator/platform, and an unexpired session. Ambiguous,
malformed, cancelled and expired callbacks cannot create an account. Sessions
expire after ten minutes, live in the shared Redis session store, and are
claimed once before account creation. Redis failure never falls back locally
in a deployment configured with Redis. Credentials are not echoed in responses
or errors. Callback fields clear after import, cancellation or expiry.

Production callbacks return `api_key`/`access_token` (also the official
camel-case aliases). An optional `expires_in` is retained. With no lifetime,
the credential is static and has no invented expiry or refresh token. This
source publishes no default token endpoint: authorization-code exchange,
device-code polling and automatic refresh are not enabled. Revoked/expired
credentials require browser reauthorization. Relinking retains the account's
region, proxy, metadata and identity; expired credential fields are replaced.

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
`step-…`), quotas, statistics and probes. No upstream balance/subscription
usage endpoint is invented. Local usage remains available; upstream quota
monitoring is unsupported. Migration 277 extends platform/probe constraints.

## Source and regression evidence

The supplied Step-Code reference at commit
`519e4de4ed2162d3667be1821cb92ada6b884e5a` uses:

- `packages/providers/src/step-provider/index.ts` and `callback-server.ts`:
  auth, static credentials, catalog and native protocol.
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

Requirement-based tests use literal official URLs/headers and adversarial
callbacks; transport tests exercise model forwarding, probes and discovery
for both account types and regions. Real-provider authorization still requires
an operator's account; mock wire tests do not assert live authorization success.
