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
are inferred. Configure channel pricing or use an available matching pricing
reference before serving paid traffic. Account connection tests require an
explicit model from that account's catalog.

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
