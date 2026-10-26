# Zhipu / GLM

Zhipu GLM accounts are API-key accounts: an operator pastes a BigModel or Z.ai
key and selects the pay-as-you-go or coding-plan endpoint. Plus additionally
supports **linking** an account through the provider's own official client
platform, ZCode, so a coding-plan subscription can be authorized instead of
pasted.

Two upstream estates are supported. They share a protocol but not a domain, and
the business APIs (customer profile, project keys) live on a third origin that is
distinct from the inference origin.

| Estate | Identifier | Business origin | Coding-plan Chat Completions | Pay-as-you-go Chat Completions | Anthropic |
| --- | --- | --- | --- | --- | --- |
| BigModel (domestic) | `bigmodel` | `https://bigmodel.cn` | `https://open.bigmodel.cn/api/coding/paas/v4` | `https://open.bigmodel.cn/api/paas/v4` | `https://open.bigmodel.cn/api/anthropic` |
| Z.ai (international) | `zai` | `https://api.z.ai` | `https://api.z.ai/api/coding/paas/v4` | `https://api.z.ai/api/paas/v4` | `https://api.z.ai/api/anthropic` |

## Account link

The account-link UI creates a new linked account. It currently has no operation
to replace the login on an existing account in place. The existing-account menu
therefore does not offer **Reauthorize account** for Zhipu; it must not send that
account to another provider's login. Start a new account link when a replacement
business login is needed. Short project-token renewal remains a separate
internal operation and does not establish business-login refresh support.
The menu also omits **Refresh token**, since the gateway has no published
business-login refresh operation for this account type.

The link is a **server-side polling handshake**. ZCode's own client authorizes in
a browser and the provider hands control back to the client through a `zcode://`
deep link; a server-hosted deployment can never receive that hand-off, so Plus
uses the same platform handshake without any inbound callback:

1. `POST /oauth/cli/init` opens a flow and returns the authorization URL.
2. The operator opens that URL in a browser and authorizes on the provider site.
3. The admin panel polls; the server polls the platform with a credential that
   **never leaves the server**.
4. Once the platform reports `ready`, the panel creates the account from the
   returned token material.

The server holds the poll credential and the flow id; the panel only ever
receives an opaque session handle. Authorization URLs are accepted only when they
are absolute `https` URLs, and the platform flow id is never taken from a client
request, so the flow cannot be redirected at another endpoint.

| Method | Path | Purpose |
| --- | --- | --- |
| `GET` | `/api/v1/admin/zhipu/oauth/capabilities` | Supported estates and plan kinds |
| `POST` | `/api/v1/admin/zhipu/oauth/start` | Open a link or re-link flow |
| `POST` | `/api/v1/admin/zhipu/oauth/poll` | Advance the flow; returns `pending` or the token material |
| `POST` | `/api/v1/admin/zhipu/oauth/exchange-code` | Fallback: redeem a pasted callback URL or bare code |
| `POST` | `/api/v1/admin/zhipu/oauth/create-from-oauth` | Resolve the plan credential and create the account |

`exchange-code` exists for environments where the polling handshake is
unavailable. It accepts either the whole callback URL or just the code, and the
BigModel estate's historical `authCode` parameter name is accepted alongside
`code`. Because a server cannot receive the provider's registered redirect, this
path requires the operator to copy the URL from the browser address bar.

Link sessions live for 15 minutes or the platform's own expiry, whichever is
shorter, and are **single-use**: one authorization cannot create two accounts.
With Redis configured, it is authoritative: reads, writes and the single-use
claim must succeed there. A local cache never bypasses a Redis failure. The
claim remains until expiry, including after an ambiguous create response, so
concurrent replicas cannot create two accounts from one authorization. Without
Redis the session is process-local; multi-instance deployments require Redis or
instance affinity. The UI discards expired credentials, cancels the session when
the proxy changes, ignores late responses after cancellation, and resumes polling
after a failed callback exchange. A create accepted before expiry still reports
its eventual result. Pasted callback state must match the original session, and
exchange must retain the session's proxy.

## Plan kinds

A linked subscription owns distinct login, project and inference material. The
operator selects its plan kind because authorization alone does not identify the
subscription to use.

| Plan kind | Inference credential | Protocol | Endpoint |
| --- | --- | --- | --- |
| `individual-coding-plan` | short-lived Project Access Token (PAT) | Chat Completions / Anthropic | estate coding + Anthropic origins |
| `team-coding-plan` | short-lived PAT for the team Key ID (`keyType=2`), with organization/project scope | Chat Completions / Anthropic | estate coding + Anthropic origins |
| `start-plan` | the ZCode token as a bearer credential | Anthropic | `https://zcode.z.ai/api/v1/zcode-plan/anthropic` |
| `off-peak` | the ZCode token plus a current coding-plan PAT and admission ticket | Anthropic | `https://zcode.z.ai/api/v1/off-peak/anthropic` |

Project resolution reads the customer profile, selects a non-team
organization/project for personal plans (preferring the default-named pair),
and finds or creates the dedicated Key ID: `zcode-api-key` for personal plans or
`zcode-team-api-key` with `keyType=2` for teams. Creation declares `usageScene=1`.
The returned `apiKey` is a public resource ID. OAuth resolution does not call
Copy Secret, reconstruct a long-lived key, or fall back to an earlier copied key.

PAT issuance uses
`POST /api/biz/v1/organization/{org}/projects/{project}/api_keys/{id}/access_tokens`
on the estate's business origin. It sends the business login token as Bearer
and declares `clientType:"zcode"` plus the selected trusted client version.
The customer lookup uses the bare login token in both estates. Personal key
list/create keeps BigModel's bare token and Z.ai's Bearer token; team list/create
uses the bare token with organization/project headers in both estates.

New linked account credentials retain the business login material, public Key ID
and organization/project. The compatibility `api_key` field for personal/team
plans contains the public Key ID; it is not a model authentication fallback.
The runtime PAT owner resolves the current token before every physical model,
probe and discovery send. PATs are held in memory, separate from stable account
identity and persisted metadata. Existing linked accounts can reconstruct public
metadata from their login rather than depend on an old copied secret.

The PAT cache is scoped to the credential owner, login generation, estate,
project, Key ID and proxy. Concurrent refreshes for that scope are merged in the
process. Issuance must include a finite future `expiresAt`; a relative TTL cannot
replace a missing absolute deadline. When a TTL is also returned, the earlier
effective deadline applies, with a refresh margin of 120 seconds plus 0–60
seconds jitter. A transient transport error, HTTP 429 or 5xx may retain an unexpired,
unrejected PAT for five seconds before another refresh attempt. It never extends
the original expiry. Authentication rejection, cancellation, disabled issuance,
malformed material and expired or explicitly rejected PATs cannot fall back to
a long-lived key. An account or login change rejects a late issuance result.

On Z.ai, the handshake's OAuth token first goes through
`POST /api/auth/z/login` to obtain the business token. BigModel already returns
a business token. PAT renewal is distinct from refreshing that login credential.
Explicitly entered API-key accounts retain their manual-key semantics; Start
Plan retains its JWT channel.

Linked accounts use `api_protocol=adaptive` with explicit per-protocol base URLs.
Native OAuth-derived project authentication stays on its allowed estate and
plan endpoints; generic URL/header configuration cannot choose another issuer.
Start Plan sends `Authorization: Bearer <ZCode token>` automatically from its
linked plan type, without an API-key authentication-scheme setting. Existing
redundant scheme declarations do not change this native channel. Off-peak uses
the same JWT bearer plus its separately acquired project PAT and ticket.

Live model discovery for linked personal and team plans uses the estate's
coding `GET /models` endpoint with the current PAT, even when inference uses the
Anthropic protocol. It captures the selected identity and project owner before
building the request; a login, plan, project or proxy change before send requires
a retry. This path does not send a public Key ID or copied secret. Start Plan and
off-peak do not expose this model-discovery path. Manual API-key accounts keep
their configured model endpoint and manually entered credential.
The public model cache includes the captured credential owner, keeping relinked
logins separate while allowing PAT rotation within the same owner.
Admin capability synchronization atomically compares the full request owner
captured by PAT preparation before saving metadata, so a concurrent relink
cannot inherit the previous project's model capabilities.

## Coding-plan quota

Linked personal and team accounts query
`GET /api/monitor/usage/quota/limit` on `https://open.bigmodel.cn` or
`https://api.z.ai`, according to the linked estate. The request uses the current
PAT as the raw `Authorization` value, without `Bearer`, and the same account's
proxy and trusted control-plane identity. Redirects and generic URL/header
overrides cannot redirect the credential. Only team plans send `?type=2` and
`bigmodel-organization`/`bigmodel-project`; personal project metadata does not
imply team membership. Off-peak accounts retain an explicit parent-project kind
(`zhipu_project_kind=personal|team`). Older off-peak accounts without that marker
must relink before quota probing rather than infer a parent from project IDs.

Only successful monitor envelopes and explicit finite, nonnegative percentages
for units `3` (5h) and `6` (weekly) are scheduling observations. Token windows
take precedence over credit windows. Missing, malformed, duplicate or unknown
windows do not become zero or get classified from reset-time ordering. An
unrecognized response preserves the previous snapshot; a recognized partial
snapshot clears omitted windows and stale reset timestamps. HTTP 401/403 rejects
the cached PAT and cannot retry with a Key ID or copied secret.

Quota persistence atomically compares the captured credential owner against the
stored account's full credentials, platform/type, proxy and active row before
merging the observation. A concurrent relink, deletion or persistence failure
produces no usable successful observation. A failed compare changes no snapshot,
cache or outbox; an outbox failure rolls the observation back.

## Off-peak idle plan

Off-peak is not a static credential: model traffic is admitted by a **ticket**
issued by the platform, and the official client's queue is unbounded.

| Operation | Method and path | Notes |
| --- | --- | --- |
| Availability | `GET /api/v1/off-peak/ticket/availability` | Take-number quota snapshot; gates new work only |
| Take | `POST /api/v1/off-peak/ticket` | Returns `ticket_id`, `state`, queue `position`, `next_poll_after` |
| Status | `POST /api/v1/off-peak/ticket/status` | Batch, up to 100 tickets |
| Settle | `POST /api/v1/off-peak/ticket/{id}/settle` | Idempotent; an empty successful 200/204 is an acknowledgement; errors retain the ticket for retry |

States are `queued`, `ready`, `active`, `expired`, `settled` and `not_found`.
A model request is admitted only while the ticket is `ready` or `active`; a
`queued` request is rejected by the platform.

**Plus bounds the wait instead of queueing forever.** An off-peak account holds at
most one ticket; a request rechecks a cached `ready` or `active` ticket with the
platform before reusing it, and
otherwise the server takes a ticket and polls until the platform promotes it,
within a configurable budget. When the budget expires the request fails with a
retryable status that reports the current queue position. A ticket that expires
while queued is re-taken once. Tickets idle for longer than the settle window are
settled in the background, which returns the take-number quota; a failed settle
is retried on the next cycle because the platform reclaims un-settled tickets by
timeout anyway.

The ticket protocol accepts the official bare JSON responses and zero-code
envelopes. Empty successful settlement responses retire the ticket exactly once;
they cannot create admission on a take request. A failed proxy connection,
timeout, proxy HTTP 407 or upstream rejection leaves the outstanding generation
available for retry. Recovery uses the original credential owner's captured
proxy and identity. A missing, mismatched or malformed bound proxy is rejected
before PAT issuance or ticket traffic; it never authorizes direct egress or a
replacement route supplied by a caller. Invalid proxy errors omit credentials.
The response contract follows official ZCode commit
`3c5b227649371507ddc4eb46ac5133afe6db1cab`,
`packages/services/src/session/offPeakServerClient.ts`.

Ticket take, status and settle HTTP 401/403 invalidate only the PAT used for that
physical call, so the next call must issue a fresh token. A delayed rejection
cannot invalidate a PAT already rotated in the same scope. HTTP 429/5xx can retain
a still-valid, unrejected token. Settlement always uses the original login,
ZCode JWT, identity and project owner, including after relinking.

| Business code | Meaning | Gateway behaviour |
| --- | --- | --- |
| `3101` | no eligible coding plan | `403`, the account cannot use the idle queue |
| `3103` | take-number quota exhausted | `429`, with the platform's reset time |
| `3102` / `3001` | ticket unusable on the model endpoint | the upstream error is returned; the next request rechecks admission and replaces expired/missing tickets |
| `3105` | queue valve on the model endpoint | `429`, with `Retry-After` |

Two documented deviations from the official client remain. The official client
binds a ticket to a whole run segment (every turn and retry) and settles it when
the segment ends; a stateless gateway has no segment boundary, so Plus reuses a
ticket across requests until it goes idle and then settles it. And the official
client waits forever while queued, re-entering the queue without consuming its
retry budget; Plus fails fast after its budget so a user request cannot occupy a
gateway slot indefinitely.

**Not supported:** the off-peak `task_id` continuation semantics. Plus issues its
own `offpeak-<UUID>` task id per ticket lifetime and never resumes a previous
run's queue position. Ticket settlement retains the acquiring account scope,
identity and proxy even if settings change. A new login/plan/proxy retires the
old ticket before acquiring a new one. PAT renewal within the original scope
does not turn the ticket into a different account's admission. Every ticket
generation is eligible for idle settlement. Forwarding and account probes require successful admission
before any model dispatch; a missing provider or failed acquisition cannot send
a caller-supplied ticket or a request without one. A request cannot reuse a ticket while it is being
settled. The acquisition budget covers lock contention and upstream calls.

## Outbound identity

Zhipu GLM accounts advertise the pinned ZCode client identity by default, for
both API-key and linked accounts. ZCode is the provider's own official client and
renders its product, version companion and runtime declaration block.
Both OAuth and API-key accounts are pinned to that family. Valid account
parameters can override the global profile; cross-family selections and the
retired `zhipu:apikey` default mapping are rejected.

The preset sends `ZCode/3.14.3` together with `X-ZCode-App-Version: 3.14.3`,
`HTTP-Referer: https://zcode.z.ai`, `X-Title: Z Code@electron`,
`X-Release-Channel: production`, and `X-ZCode-Agent: glm`. Its five host
facts (`X-Client-Language`, `X-Client-Timezone`, `X-Platform`, `X-Os-Category`,
`X-Os-Version`) are generated once, persisted and editable in **System Settings
→ Outbound identity → GLM · ZCode**, with per-account overrides. Version changes
update UA and its companion together while preserving host and product facts.
No Codex `Originator` or standalone `Version` header is sent. Inference adds the pinned per-protocol AI SDK UA suffixes; control-plane
calls omit those suffixes and `X-ZCode-Agent`. Control OS version follows
`os.version()`, while inference follows `os.release()`. No telemetry device ID
is invented. See [Outbound identity](../OUTBOUND_IDENTITY.md)
for exact defaults, source evidence and the source-priority matrix.

The account-link handshake and the credential-derivation calls are pre-account
authorization operations. The session retains one native identity across start,
poll, exchange and credential derivation, including Redis round trips. The
created account pins that identity. Runtime PAT requests reuse the same
credential-owner identity, and their body client version stays coherent with
its trusted declarations. Short-lived authentication rotation does not select
an identity source or alter the SDK fingerprint. Both OpenAI-compatible and
Anthropic forwarding apply the current plan credential before sending.

## Known limitations

- **Start-plan and off-peak are Anthropic-protocol only.** Their endpoints exist
  only under the ZCode plan gateway, so a Chat Completions request routed to such
  an account falls back to the estate's default Chat Completions origin and will
  not be admitted. The official client also uses `anthropic-messages` for both.
- **Login renewal remains separate.** The short-lived PAT is renewed at request
  time; the stored provider refresh token is not exchanged by a ZCode background
  refresher. An invalid business login requires reauthorization.
- **Public metadata is scoped.** Persisted project/Key IDs are not secrets or a
  substitute for a valid login. A removed or inaccessible project cannot use an
  old copied key to recover access.
- **Team plans need their scope.** A team subscription can own several projects,
  so the organization and project must be supplied when linking.

## Unaffected areas

Manual API-key endpoint/authentication choices, the Chat Completions / Responses /
Anthropic adapters, `anthropic-version`, beta handling, billing, scheduling,
proxying and the ingress security-audit boundary retain their existing contracts.
PAT resolution does not move model dispatch ahead of ingress audit. Count-token
requests for GLM remain a local estimate and send no upstream request.

## Protocol source evidence

The PAT migration was checked against supplied ZCode commit
`3c5b227649371507ddc4eb46ac5133afe6db1cab` on 2026-10-25:
`packages/shared/src/project-access-token-client.ts`,
`project-access-token.ts`, `project-access-token-response.ts`, and
`docs/model-provider-project-access-token.md`. Compiled identity/SDK defaults
remain the separately pinned declarations in
[Outbound identity](../OUTBOUND_IDENTITY.md); this protocol migration does not
implicitly update their versions.

Requirement-based tests cover the actual issuance path and headers, absence of
Copy Secret, personal/team estates, expiry and rejection, concurrent refresh,
owner changes, cancellation and unchanged manual-key behavior. These use mock
transports; real-account PAT issuance, long off-peak queues and provider
acceptance require separate authorized upstream evidence.
