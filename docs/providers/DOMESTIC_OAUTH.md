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
credential refresh and authentication boundary. No grant is sent to the public
API-key billing/quota or model-discovery endpoints; model selection uses the
existing provider catalog. Live model sync remains unsupported for these OAuth
accounts. OAuth-specific quota discovery is not added by this login feature.

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

Completion claims the session before the account write. Repeated successful
completion returns its receipt; concurrent or ambiguous writes cannot create a
second account. If an account write succeeds but its receipt cannot be saved,
check the account list before starting another authorization. A failed save
requires a new login; the grant is never returned to the browser for replay.

## Source evidence and regression coverage

Protocols were checked against local official client sources:

- `dsh-desktop/vendor/dsh-runtime/0.2.0-rc.2/`: vendored
  `dsh-deepseek-account-platform` (`types/index.js`, `types/protocol.js`),
  `dsh-deepseek-account` platform headers and `dsh-llm-deepseek-account` auth.
- `kimi-code/packages/oauth/src/`: `oauth.ts`, `constants.ts`, `region.ts`,
  `identity.ts`, `managed-kimi-code.ts`.
- `minimax-code/packages/oauth-core/src/`: `oauth-client.ts`, `endpoint-config.ts`,
  `contracts.ts`; `packages/config/src/config.ts` inference destinations and
  `packages/local-runtime/src/runtime/model-resolver-helpers.ts` request headers.

`internal/pkg/cnoauth` tests cover wire formats, PKCE/state, regions, errors,
refresh, scope validation and Redis/local session isolation. Service tests cover
completion replay, credential ownership, relink/cancel, native destination/auth,
identity and refresh. Frontend tests cover polling intervals, expiry, cancellation,
stale in-flight responses, callback entry and account creation. Existing identity,
Codex and security-audit regressions remain mandatory.
