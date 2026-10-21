# DeepSeek, Kimi and MiniMax account login

Account creation exposes **OAuth login** alongside API Key configuration for
these three platforms. Editing an OAuth account exposes **Reauthorize account**.
The existing name, groups, priority, concurrency and selected proxy are used for
creation. Relinking replaces credentials on the selected account and preserves
its other settings, while clearing credential errors and scheduling cooldowns. Choose the account's China/global region before login;
DeepSeek currently has one supported region.

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

Kimi and MiniMax recover one explicit upstream 401 on the selected account before
account failover. A replayable request forces refresh only if the rejected access
token is still current; a concurrent rotation is reused. The retry retains the
same credential owner, proxy and outbound identity. A second 401 or an invalid
refresh grant makes that grant unavailable for scheduling; shared refresh,
locking or persistence failures stop the provider attempt without quarantining
other accounts. Non-replayable bodies are never resent. Refresh success is
conditionally written against the complete grant and proxy used for refresh,
so a concurrent reauthorization cannot be overwritten. A configured Redis
refresh-lock failure stops refresh rather than allowing uncoordinated rotation.

DeepSeek accepts a loopback callback, not a remote web callback. This panel uses
`http://127.0.0.1:53682/oauth/callback`. After authorizing, copy the **complete URL**
from the browser address bar and paste it into the panel, even if the browser
reports that the local page cannot load. The server validates the exact loopback
origin/path and single matching `state` and `code`; it never fetches the pasted
URL. No listener or public callback route is required. DeepSeek's device ID is
retained on relink; device model and OS release describe the running server.

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
credential refresh and authentication boundary. DeepSeek OAuth discovery calls `GET https://api.deepseek.com/models` with
`x-dsh-auth-token`; Kimi uses its regional `GET /coding/v1/models` with Bearer
auth. Native OAuth origins stay fixed and redirects are disabled. These are
account catalogs. Kimi managed usage calls its fixed regional
`GET /coding/v1/usages` using the same OAuth grant, device snapshot and one-time
401 recovery. API-key billing endpoints never receive native grants. MiniMax
OAuth model sync remains unsupported. Its coding quota follows the official
[`matrix-account-client.ts` at the reviewed baseline](https://github.com/MiniMax-AI/minimax-code/blob/5821f7c9b0363a9b97a57fff3deaa4cd2da165a4/packages/tui/src/account/matrix-account-client.ts):
identity and workspace/membership lookup on `https://agent.minimaxi.com` (China)
or `https://agent.minimax.io` (global) select the personal workspace
(`workspace_type: 0`) and its `op_group_id`. The same OAuth grant then calls
`GET /v1/api/openplatform/coding_plan/remains` with `X-Group-Id` on the fixed
`https://www.minimaxi.com` or `https://platform.minimax.io` origin respectively.
Team groups, configured inference URLs and generic headers cannot replace this
personal billing scope. Redirects remain disabled.

Kimi, Zhipu and MiniMax coding-plan probes isolate concurrent queries and monitor
caches by account ID, platform, account type, the complete credential document,
and selected proxy. A Kimi 401 recovery binds the result to the actual refreshed
grant used by the successful query. A late response from a replaced grant cannot
overwrite quota state or publish scheduler invalidation: both persist atomically
only while that exact credential/proxy snapshot still matches the account.
`success` describes the provider response; `persisted` is true only after that
comparison and durable write succeed. Missing conditional storage never falls
back to an unconditional quota write.

Scheduling thresholds and quota-derived reset cooldowns require a matching
`<provider>_quota_generation` binding. Existing unbound windows and windows from a
previous authorization remain observational until a successful probe records the
current generation. Reauthorization and proxy changes therefore cannot inherit
the previous grant's exhausted windows. A successful recognized response replaces
the provider's windows; omitted windows cannot retain an older percentage/reset.
The binding stores a digest, never raw credential or proxy secrets. Command Code,
Cline and OpenCode Go keep their own existing quota contracts.

MiniMax OAuth exposes recognized five-hour and weekly windows with reset times
in the account table and quota monitor. `status=3` stays explicitly unlimited,
not a fabricated utilization. Matching finite snapshots feed the existing
scheduling thresholds; native OAuth is additionally excluded at full exhaustion
until its known reset even if early-pause thresholds are disabled, allowing a
sticky session to move to an available account temporarily. Quota exhaustion never permanently disables the
credential. Query failures or unrecognized responses remain unknown and retain
the last recognized snapshot without adding credential errors. Credits are
independent of token-plan quota; missing Credits, a missing plan or unknown quota
does not establish an invalid grant.

The default inference allowlist and `deploy/config.example.yaml` include the
hosts above. Deployments replacing `security.url_allowlist.upstream_hosts` must
add the required exact inference and account/quota hosts to their own list.

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
`MiniMaxAgent` family and declares a gateway-generated session UUID, the official
default agent `main`, and the selected timezone UTC offset in seconds in its
`X-Mavis-*` protocol headers. DeepSeek's Harness session header and MiniMax's
Mavis session header seed sticky routing only within their respective groups.
Outbound session UUIDs are isolated by authenticated tenant and credential owner
and remain stable across conversation turns; switching accounts changes the
outbound UUID. These protocol fields never select the outbound client identity.

Native Anthropic streams defer structural frames until meaningful output or an
unrecognized frame, with a 64 KiB prefix limit and a five-second maximum wait
(a shorter configured keepalive interval takes precedence). Authentication/quota errors before that boundary enter the
same recovery path as HTTP errors, including one Kimi/MiniMax OAuth refresh on
the selected owner before failover. Unknown content is preserved conservatively.
Errors after output never replay the request. Buffered and streaming protocol
conversions report incomplete/error streams as failures and retain observed
usage; missing upstream completion never synthesizes a successful terminal frame.
A complete `message_stop` frame finishes conversion without waiting for the
upstream socket to close. Timeouts before completion retain observed usage.

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

Completion claims the session before the account write. Repeated successful
completion returns its receipt; concurrent or ambiguous writes cannot create a
second account. If an account write succeeds but its receipt cannot be saved,
check the account list before starting another authorization. A failed save
requires a new login; the grant is never returned to the browser for replay.

## Source evidence and regression coverage

Protocols were checked against local official client sources:

- [deepseek-harness](../OFFICIAL_AGENT_BASELINES.md):
  `packages/credentials/deepseek-account-platform/src/{types,protocol}.ts`,
  account platform declarations and `llm-deepseek-account` auth/listing.
- `kimi-code/packages/oauth/src/`: `oauth.ts`, `constants.ts`, `region.ts`,
  `identity.ts`, `managed-kimi-code.ts`, `managed-usage.ts`;
  `packages/agent-core-v2/src/human/credentials/credentials.ts` same-account
  authentication recovery.
- `minimax-code/packages/oauth-core/src/`: `oauth-client.ts`, `endpoint-config.ts`,
  `contracts.ts`; `packages/config/src/config.ts` inference destinations and
  `packages/local-runtime-v2/src/service/model-system/resolution/` request headers;
  [`packages/tui/src/account/matrix-account-client.ts`](https://github.com/MiniMax-AI/minimax-code/blob/5821f7c9b0363a9b97a57fff3deaa4cd2da165a4/packages/tui/src/account/matrix-account-client.ts)
  personal workspace, account lookup and regional token-plan quota.

`internal/pkg/cnoauth` tests cover wire formats, PKCE/state, regions, errors,
refresh, scope validation and Redis/local session isolation. Service tests cover
completion replay, credential ownership, relink/cancel, native destination/auth,
identity and refresh. Frontend tests cover polling intervals, expiry, cancellation,
stale in-flight responses, callback entry and account creation. Existing identity,
Codex and security-audit regressions remain mandatory.
