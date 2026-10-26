# DeepSeek, Kimi and MiniMax account login

Account creation exposes **OAuth login** alongside API Key configuration for
these three platforms. Editing an OAuth account exposes **Reauthorize account**.
The existing name, groups, priority, concurrency and selected proxy are used for
creation. Relinking replaces credentials on the selected account and preserves
its other settings, while clearing credential errors and scheduling cooldowns. Choose the account's China/global region before login;
DeepSeek currently has one supported region.

The account menu's **Reauthorize account** uses the same native panel as the
editor for DeepSeek, Kimi, MiniMax and StepFun; it never enters the Claude
authorization flow. The service receives the saved account ID and region and
captures that account's persisted proxy. Changing the account, its credentials
or saved route, closing/reopening the modal, or disposing it revokes the old
panel generation. A late completion cannot trigger another account reload, and
a late reload cannot report success or close the replacement flow. StepFun's
existing-account region remains fixed; its independent session rules are in
[StepFun](STEPFUN.md).
DeepSeek has no published refresh operation, so its menu offers native
reauthorization and omits **Refresh token**. Kimi, MiniMax and StepFun retain
their supported refresh actions; an absent StepFun Passport session is still
handled by the backend's existing account-session rules.

| Platform | Official login | Inference authentication | Refresh |
| --- | --- | --- | --- |
| DeepSeek | Browser authorization code with S256 PKCE and state | `x-dsh-auth-token` | Reauthorize in the editor; the official protocol publishes no refresh operation |
| Kimi | Device authorization, client `17e5f671-d194-4dfb-9706-5516cb48c098` | `Authorization: Bearer` | Background pool, request path and account refresh action |
| MiniMax | Device authorization with S256 PKCE, client `mcode-public`, scope `agent.default`, audience `agent-backend` | `Authorization: Bearer` | Background pool, request path and account refresh action |

Kimi and MiniMax display the user code and authorization link, then poll at the
provider's interval (including `slow_down`). Cancellation, rejection and expiry
stop the flow. MiniMax accepts both the standard `device_code` response and its
account service's `user_code` polling response, whose interval is milliseconds.
Token validation requires the MiniMax `agent.default` scope, including scope
arrays or JWT `scope`/`scp` declarations.

## Refresh deadlines

OAuth refresh checks the actual context deadline before accepting provider
credentials, even if timer contention delays cancellation notification. A late
result cannot be persisted. Caller/cycle expiry stops refresh without adding
account errors or cooldowns. A refresh already persisted within its attempt may
finish bounded detached cleanup without being retried or counted as a failure.

Native refresh binds the request to its complete credential owner before the
coordinator rereads the account. Changed credentials, platform/type or proxy
require the next request to reload that generation. A successful issuer response
is accepted only through an atomic comparison against the full old owner, then
verified against the durable new credentials. A different refresh token after
`invalid_grant` does not prove the same login and cannot silently switch owners.
The atomic credential repository port is required before issuer I/O.

The captured proxy binding is mandatory whenever `proxy_id` is present. A
missing, mismatched or malformed hydrated proxy stops login, refresh, inference
and control requests before provider I/O; it cannot select host networking
or repair the current operation by rereading a mutable proxy row. Only an
explicitly unbound account uses direct egress. HTTP proxy refusal, timeout or
authentication failure never enables a direct retry. Proxy expiry fallback is a
separate saved-binding operation and cannot change an in-flight owner's route.

The same rule applies to API Key balance and coding-plan quota probes, including
DeepSeek/Kimi payg balances and Kimi/Zhipu coding quotas. The service captures the
loaded credential owner and validates its proxy while computing that owner's
fingerprint, before concurrent probes can join a shared singleflight operation.
It sends through that captured route and never rereads a mutable proxy row to
repair a missing or invalid binding. Invalid bindings leave credentials and
existing observations unchanged. A saved unbinding permits direct egress even
when the loaded account still contains a stale proxy association. Native OAuth
and API Key control paths both preserve this distinction.

API Key balance and coding-plan observations use the same full-owner SQL
comparison as native controls. An old response after key, nested credential,
binding or proxy-route changes cannot stamp the replacement owner's snapshot
fresh. Failed persistence is not a successful or credential-valid quota result.
Wallet scheduling additionally compares the previous scheduling/cooldown state;
an old wallet result cannot pause a new key or clear another subsystem's cooldown.

Background native refresh requires the unified coordinator and refresh executor;
without either, it does not call the issuer. Native refresh failure ends that
attempt without setting account errors, cooldowns or token-cache invalidations
by account ID, which could otherwise penalize a concurrently relinked owner.

DeepSeek accepts a loopback callback, not a remote web callback. This panel uses
`http://127.0.0.1:53682/oauth/callback`. After authorizing, copy the **complete URL**
from the browser address bar and paste it into the panel, even if the browser
reports that the local page cannot load. The server validates the exact loopback
origin/path and single matching `state` and `code`; it never fetches the pasted
URL. No listener or public callback route is required. DeepSeek's device ID is
retained on relink; device model and OS release use the pinned neutral-host
fingerprint documented in Outbound identity.

## Destinations and protocol

Native OAuth accounts carry `oauth_provider` and `oauth_region` in their
credentials. Their upstream protocol is Anthropic Messages; the existing
Chat Completions and Responses adapters convert incoming requests to Messages.
The provider catalog, model mappings, group admission, billing and ingress audit
continue to apply. These accounts do not use the OpenAI/Claude token clients.

| Platform / region | Authorization origin | Messages URL |
| --- | --- | --- |
| DeepSeek | `https://platform.deepseek.com` (`/auth-api/v0/dsh/auth_init`, `auth_exchange`) | `https://api.deepseek.com/anthropic/v1/messages` |
| Kimi China | `https://auth.kimi.com` (`/api/oauth/device_authorization`, `/api/oauth/token`) | `https://api.kimi.com/coding/v1/messages?beta=true` |
| Kimi global | `https://auth.kimi.ai` (same paths) | `https://api.kimi.ai/coding/v1/messages?beta=true` |
| MiniMax China | `https://account.minimax.cn` (`/oauth2/device/code`, `/oauth2/token`) | `https://agent.minimax.cn/mavis/api/v1/llm/v1/messages` |
| MiniMax global | `https://account.minimax.io` (same paths) | `https://agent.minimax.io/mavis/api/v1/llm/v1/messages` |

Native grants cannot be rerouted by `base_url`, `api_base_urls`, `api_protocol`,
inbound authentication headers or generic header overrides. OAuth and inference
requests do not follow redirects. Tests and actual forwarding share the native
credential refresh and authentication boundary. Each native control operation
uses the explicitly supported official endpoint below; generic account URLs and
headers cannot redirect its grant to another issuer or region.

| OAuth account | Control operation | Authentication |
| --- | --- | --- |
| Kimi China/global | `GET https://api.kimi.{com|ai}/coding/v1/models` | Bearer access token; same coordinated refresh as Messages |
| Kimi China/global | `GET https://api.kimi.{com|ai}/coding/v1/usages` | Bearer access token; same coordinated refresh as Messages |
| DeepSeek | `GET https://api.deepseek.com/models` | `x-dsh-auth-token` |
| DeepSeek | `GET https://platform.deepseek.com/api/v0/users/get_user_summary` | `x-dsh-auth-token` |

Admin discovery and the public model-list fetch use this same native control
authentication before their physical send. Their cache keys include the captured
credential owner, so relinking cannot reuse another login's model list. Step
Plan's public list likewise retains its fixed regional endpoint and static model
key through the native request preparer.
Native admin capability synchronization saves metadata only when the full
credential owner that produced the model response still matches atomically.
Coordinated refresh uses the actual refreshed owner for this comparison. A
concurrent relink cannot receive the previous login's capability snapshot.

DeepSeek native discovery checks each row's
`client_requirements["deepseek-harness"].min_version` against the outbound
identity selected for that request. A newer lower-priority identity cannot
satisfy the selected client's requirement. Unreadable requirements, absent or
invalid `api_capabilities.anthropic_messages`, unsupported input modalities,
missing text output and invalid declared capacities exclude only that row.
Admin IDs, capability metadata and the public model list share this filtering;
public cache entries are isolated by the selected identity and credential owner.
This compatibility check does not advance the compiled identity version.

Model discovery retains IDs separately from capability completeness. Kimi's
`reasoning` may be a boolean or an object with `type` and
`effort.valid/default`; object parsing preserves context, output and modalities
from sibling fields. Unknown reasoning declarations remain unknown instead of
discarding those sibling facts. The generic boolean catalog contract remains
available for other providers.

Kimi native usage projects recognized `usages.limit_5h`, `limit_7d` and
`limit_month_total` ratios into 5h, weekly and monthly percentages, with the
reported `reset_time`. Absent or invalid observations do not become zero.
The official client's separate monthly-code and booster-wallet dimensions are
not implied by those three snapshot windows. Unrecognized or unsuccessful
native usage responses preserve the existing snapshot. MiniMax OAuth currently
has no native live model/usage discovery in this feature; its provider catalog
and explicit model mappings remain available.

A recognized partial usage snapshot clears omitted windows and their old reset
timestamps before advancing freshness, for both native and API Key controls.
This is the complete single-response snapshot contract; independently failed
endpoints in other providers' multi-request controls retain their own recovery
rules.
API Key quota envelopes must be valid JSON and contain a recognized finite
window. Kimi counter windows require finite `limit`/`remaining` and a positive
limit; a zero denominator remains unknown. Zhipu requires a finite nonnegative
`percentage`; invalid token windows cannot fall back to credit percentages.
Actual zero use and overuse remain representable. Unknown or malformed responses
preserve the previous snapshot and timestamp.

Kimi API Key wallet success requires `code: 0`, `status: true` and a finite
`data.available_balance`, following the [official balance response](https://platform.kimi.com/docs/api/balance).
Missing or unsuccessful envelopes cannot manufacture a zero balance or clear
low-balance state; a valid zero balance is still a successful observation.

API Key controls do not follow redirects. They accept only complete responses
within 256 KiB; failed reads or oversized/truncated JSON cannot persist a
snapshot or trigger scheduling. DeepSeek API Key wallets validate the complete
currency list and boolean availability: a damaged sibling invalidates the
observation, and absent/invalid currencies never default to CNY. The accepted
currency strings are `CNY` and `USD`, as defined by the
[official wallet schema](https://api-docs.deepseek.com/api/get-user-balance/).

Native quota and wallet observations are
saved with an atomic comparison of the captured owner's full credentials,
platform/type, proxy and active account row. Concurrent relinking or deletion
cannot attach an old result to the replacement account. A failed comparison
updates no snapshot, cache or outbox; an outbox failure rolls the observation
back, and the result is not a usable successful observation.

DeepSeek account-wallet queries retain recharge and bonus wallets separately,
including each CNY/USD currency. They do not convert currencies or relabel a
failed wallet query as a zero balance. This account protocol is separate from
the API-key `/user/balance` protocol. Availability and scheduling thresholds add
recharge and bonus amounts only within the same currency; the displayed wallets
remain separate.

Wallet-based scheduling uses the owner captured by the actual wallet request,
including when the periodic account listing predates a reauthorization. Pause
and recovery compare that complete owner, manual scheduling state and previous
temporary cooldown atomically with scheduler invalidation. A replacement login,
manual scheduling change or another subsystem's newer cooldown rejects the old
decision without pausing or clearing the changed account.

DeepSeek/MiniMax Files upload, metadata, expiry and deletion are outside the
exposed gateway routes. Preserving a native Messages file reference or an inline
media block does not provide that upload lifecycle, nor establish that every
Chat/Responses conversion supports the same media. DeepSeek incremental
`tool_addition`/`tool_removal` blocks and unknown provider fields retain the
existing native pass-through and canonical audit contract: recognized sibling
content remains auditable; unextractable content does not become an audit block.

The default inference allowlist and `deploy/config.example.yaml` include the
hosts above. Deployments replacing `security.url_allowlist.upstream_hosts` must
add the required exact inference hosts to their own list.

The selected [outbound identity](../OUTBOUND_IDENTITY.md) is snapshotted for the
whole authorization session, then saved with the account. In particular Kimi's
device identity is consistent across login, refresh and inference. Relinking uses
the credential owner's account identity. DeepSeek's platform-only `x-client-*`
headers describe a web login and use the selected version, language (`zh_CN` or
`en_US`) and timezone offset in seconds east of UTC. The defaults are Chinese and
UTC; language/timezone dropdowns configure them globally or per account. The
`auth_init` body locale matches its header; the offset is captured before login
and retained through callback exchange. The bundle ID is officially empty;
inference retains its Harness UA. MiniMax inference retains the versionless
`MiniMaxAgent` family and declares a gateway-generated per-request session
UUID, the official default agent `main`, and the selected timezone UTC offset in seconds in its `X-Mavis-*` protocol headers. They are retained when
the same request is retried and are never global identity settings.

## Admin API and session lifecycle

`POST /api/v1/admin/cn/oauth/{deepseek|kimi|minimax}/{action}` requires the existing
admin authentication and a positive authenticated administrator ID:

- `start`: `{region, proxy_id?, account_id?}`. Existing accounts bind their stored
  proxy. Returns `session_id`, authorization URL, optional user code, expiry,
  polling interval and status.
- `poll` / `exchange`: `{session_id, callback?}`. Only `exchange` needs the
  complete DeepSeek callback. Responses contain status, never provider tokens.
- `cancel`: `{session_id}` invalidates local authorization material. It does not
  revoke an already issued provider grant.
- `complete`: `{session_id, name?, concurrency?, priority?, group_ids?}` saves the
  server-held grant and returns the account ID. Existing-account sessions relink
  their bound account; request data cannot change that binding.

Sessions bind administrator, provider, region, proxy and identity. They last at
most ten minutes (or the provider's shorter expiry). Redis stores serialized
state across replicas, with bounded upstream requests and a mutation lease. A
Redis outage fails closed. A bounded in-memory store is used only when Redis is
not configured, and cannot resume sessions across process restarts/replicas.

Reauthorization captures the existing account's complete credentials, hydrated
proxy, scheduling flag and recoverable error/cooldown state before the login
request. Completion atomically compares that owner before replacing credentials
and clearing those errors and cooldowns; changing the proxy row, binding,
credentials, manual scheduling flag or recoverable state requires a new login.
An old poll or completion cannot overwrite those later changes. Completion does
not clear the OpenAI-only in-memory scheduling blocker. Existing-account
sessions created before this owner snapshot was available must restart login.

Changing the selected proxy in the account panel cancels the current flow. A
late start is discarded and cancelled server-side; late poll, exchange or save
results cannot replace the new flow or emit a completed account. Step's model
preview uses the login session's captured proxy URL, including its authentication,
without rereading the current proxy row.

Completion claims the session before the account write. Repeated successful
completion returns its receipt; concurrent or ambiguous writes cannot create a
second account. If an account write succeeds but its receipt cannot be saved,
check the account list before starting another authorization. A failed save
requires a new login; the grant is never returned to the browser for replay.
Cancellation, terminal authorization errors and a saved completion receipt
discard the old credential-owner snapshot from the session.

## Source evidence and regression coverage

The 2026-10-25 protocol review used these supplied official-client snapshots:

- DeepSeek Harness `22ddee3792402c3e70ec5f86a63c3e3e895a2f60`:
  `packages/llm/llm-deepseek/src/{listing,listed-catalog}.ts`, `messages-api.ts`, `files-api.ts`,
  and `packages/credentials/deepseek-account-platform/src/{protocol,details}.ts`.
- Kimi Code `7ccac2b45b8a3481c76f771bb99c043b562de2c5`:
  `packages/oauth/src/{oauth,constants,region,identity,managed-kimi-code,managed-usage}.ts`.
- MiniMax Code `da22f11c55d4748155d7162c1030414228a00565`:
  `packages/oauth-core/src/{oauth-client,endpoint-config,contracts}.ts`,
  `packages/config/src/minimax-model-catalog.ts`, and local-runtime inference
  destination/header construction.

The compiled identity defaults remain owned by
[Outbound identity](../OUTBOUND_IDENTITY.md). Reviewing a newer source package
version does not automatically update its selected wire identity or SDK pins.

`internal/pkg/cnoauth` tests cover wire formats, PKCE/state, regions, errors,
refresh, scope validation and Redis/local session isolation. Service tests cover
completion replay, credential ownership, relink/cancel, native destination/auth,
identity and refresh. Frontend tests cover polling intervals, expiry, cancellation,
stale in-flight responses, callback entry and account creation. Existing identity,
Codex and security-audit regressions remain mandatory. Mock protocol checks do
not establish real-account authorization or every client attachment workflow;
those require separate authorized upstream/client acceptance evidence.
