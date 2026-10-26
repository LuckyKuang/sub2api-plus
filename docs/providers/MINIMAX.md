# MiniMax

Native OAuth login is available in account creation and editing. See
[login, refresh, endpoints and operational details](DOMESTIC_OAUTH.md).

MiniMax supports API-key and native OAuth accounts integrated with account/group selection, model
listing, platform quotas, channel monitoring and composite routing. It uses
the OpenAI-compatible gateway; native OAuth accounts use the official MiniMax
Code login and managed Anthropic Messages endpoint.
Account protocol selection follows the existing Chat Completions, Responses,
Messages and adaptive protocol adapters; content audit occurs before forwarding
or protocol transformation.

## Coding-plan quota origins

Native OAuth coding accounts use the official account flow from
[`matrix-account-client.ts` at the reviewed baseline](https://github.com/MiniMax-AI/minimax-code/blob/da22f11c55d4748155d7162c1030414228a00565/packages/tui/src/account/matrix-account-client.ts).
Account identity and workspace/membership lookup resolve the personal workspace
(`workspace_type: 0`); its `op_group_id` supplies the quota request's
`X-Group-Id`. Team workspaces or unrelated membership groups cannot substitute
for that personal billing context.

| OAuth region | Account origin | Quota endpoint |
| --- | --- | --- |
| China | `https://agent.minimaxi.com` | `https://www.minimaxi.com/v1/api/openplatform/coding_plan/remains` |
| Global | `https://agent.minimax.io` | `https://platform.minimax.io/v1/api/openplatform/coding_plan/remains` |

The query reuses the selected OAuth grant, proxy and trusted `minimax` identity.
These origins are fixed by region; `base_url`, API-key inference hosts and
generic header overrides cannot redirect the grant. Redirects are disabled.
Matrix account calls render the official `MiniMaxCode` User-Agent from the same
selected owner snapshot. Managed inference and quota remains retain
`MiniMaxAgent`; BYOK inference retains `Anthropic/JS 0.91.1`. Matrix is a fixed
destination profile, not an additional selectable identity family. OAuth/token
and quota remains retain a trusted product UA where official fetch omits one.

Native Messages requests use `Accept: application/json` and
`anthropic-dangerous-direct-browser-access: true`, matching the official Pi
adapter. The gateway strips foreign Claude/Codex session and SDK helper headers.
`X-Mavis-Session-Id` uses `mvs_<32 hexadecimal UUIDv4 characters>`. Explicit
tenant/conversation/owner mappings are persisted atomically; retries and resumed
turns reuse them, while failover uses the new owner. Without a reliable session
signal, only the current logical request shares an ID.
New authorizations persist an internal `oauth_login_id`; access/refresh token
rotation retains the session, while relinking the same row or replacing an API
key uses a new owner mapping. Legacy grants keep their account-row scope until
reauthorization. The internal login ID is never sent upstream.

API-key quota requests are enabled only for coding-mode accounts whose
configured inference URL has an approved HTTPS hostname:

| Inference hostname | Quota endpoint |
| --- | --- |
| `api.minimax.io` | `https://api.minimax.io/v1/api/openplatform/coding_plan/remains` |
| `api.minimaxi.com`, `api.minimax.com` | `https://api.minimaxi.com/v1/api/openplatform/coding_plan/remains` |

An omitted port or HTTPS port 443 is accepted. Userinfo, HTTP, other ports,
lookalike suffixes and third-party hosts containing a provider name in the path
or query do not qualify. The provider key must not be sent to an official quota
endpoint merely because a configured URL contains a provider-name substring.
Custom relays consequently do not get automatic official coding-plan queries.

Quota observations expose supported five-hour/weekly tiers, with reset times
normalized from the supported seconds/milliseconds formats. Unknown or malformed
observations do not invent available quota. Pay-as-you-go balance detection
continues to use supported upstream insufficient-balance responses; it does not
claim an automatic official balance endpoint.

OAuth and API-key quota flights, monitor caches and scheduling windows are bound
to the complete credential/proxy generation. The provider response and scheduler
invalidation persist together only if that exact grant still matches; a late
query cannot overwrite a replacement login or key. Legacy unbound windows do
not pause scheduling before the next successful probe. See
[quota snapshot ownership](DOMESTIC_OAUTH.md).

The account table shows recognized five-hour and weekly usage and reset times.
Remaining percentages and legacy `*_usage_count` values both describe remaining
quota; the first primary model supplies the text plan windows. OAuth windows
with `status=3` carry `unlimited: true` in account/monitor APIs and render as
unlimited, with no finite percentage or exhausted-window penalty.
Existing scheduling thresholds consume only a matching quota snapshot. Native
OAuth also excludes a finite window at 100% usage with a future reset even when
the optional early-pause threshold is disabled (100). This moves a sticky session
to another available account and preserves that replacement's affinity; the
original account becomes eligible again after reset. This rule avoids automatic
Credits consumption after observed plan exhaustion. A missing reset cannot
invent an indefinite pause; upstream exhaustion still triggers a bounded cooldown.
Exhaustion does not permanently disable the credential. Failed,
missing or unrecognized quota responses remain unknown, preserve the last
recognized snapshot and do not invalidate or quarantine the account. MiniMax
Credits are independent from token-plan windows: absent Credits, no token plan
or unavailable quota is not evidence of an invalid OAuth credential. The quota
cell does not present Credits as a plan window or claim a Credits balance probe.
The official [error classifier](https://github.com/MiniMax-AI/minimax-code/blob/da22f11c55d4748155d7162c1030414228a00565/packages/shared/src/llm-error-classifier.ts)
identifies structured MiniMax codes `2056` and `2067` as plan exhaustion
(the latter has automatic Credits disabled); `1400010161` means insufficient
Credits. These trigger recoverable cooldown/failover, including HTTP 500 wrappers,
without permanently marking credentials invalid. Cooldowns compare the complete
transmitted credentials and proxy ID before writing; process-local blocking is
published only after that conditional write succeeds. A refreshed grant is
resolved from transport evidence, and late responses cannot pause a replacement
login or evict its cached token.

All four OAuth account/quota hosts are in the default URL allowlist and deployment
example. Deployments that replace `security.url_allowlist.upstream_hosts` must
add their region's exact account and quota hosts. Active accounts are checked by
the existing CN quota background job (`gateway.cn_providers.balance_check_enabled`,
default on, with `balance_check_interval_minutes` defaulting to 10); the account
table and monitor also support manual queries. Detection is periodic, so this is
not a per-inference spending reservation or a guarantee against requests already in flight.

## Outbound identity

MiniMax Code's managed-login and BYOK paths have different defaults:

| Account | Default preset | User-Agent |
| --- | --- | --- |
| OAuth | `minimax` | `MiniMaxAgent` |
| API Key | `minimax_apikey` | `Anthropic/JS 0.91.1` |

Both inference identities include the pinned `X-Stainless-*` SDK block described
in [Outbound identity](../OUTBOUND_IDENTITY.md). Only managed OAuth is
versionless. Both account types retain their corresponding official family;
neither can select another preset. Valid account parameters may override the
global profile. SDK identity is pinned independently of product
versions and cannot be changed through generic header overrides.

Messages requests carry `X-Mavis-Session-Id`, `X-Mavis-Agent-Id: main` and the host
UTC offset in seconds. The same request/owner retains these across retries;
failover gets the next owner's state. OAuth/refresh uses the trusted managed UA
without inference SDK headers. Upstream's fetch-only login has no explicit
product UA; retaining the gateway's trusted UA there is an intentional identity
policy difference, not a claim about an upstream SDK default.

Settings show both presets separately. `MiniMaxCode` in OpenCode Go and
`MiniMax-Code` in the GitHub downloader describe other destinations. See the
[source and priority matrix](../OUTBOUND_IDENTITY.md#domestic-provider-source-evidence-and-source-priority).

Managed OAuth requests recover one explicit upstream 401 on the same account
before failover. Refresh is tied to the rejected access-token generation, so a
concurrent rotation is reused. The retry preserves the request body, selected
identity and proxy. A second rejection or invalid refresh grant makes that grant
unavailable; shared refresh/locking/storage failures stop the attempt without
marking the account invalid. Bodies without a safe replay source are never sent
twice, and refresh results cannot overwrite a concurrently reauthorized grant.
