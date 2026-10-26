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
| `individual-coding-plan` | short-lived project access token (PAT) | Chat Completions / Anthropic | estate coding + Anthropic origins |
| `team-coding-plan` | short-lived team project PAT (`keyType=2` location) plus `bigmodel-organization` / `bigmodel-project` | Chat Completions / Anthropic | estate coding + Anthropic origins |
| `start-plan` | the ZCode token as a bearer credential | Anthropic | `https://zcode.z.ai/api/v1/zcode-plan/anthropic` |
| `off-peak` | the ZCode token as a bearer credential plus the project PAT | Anthropic | `https://zcode.z.ai/api/v1/off-peak/anthropic` |

Linked coding accounts resolve a non-team organization/project for personal
plans, preferring the default-named visible pair, or use explicit team scope.
They find/create the official `zcode-api-key` (personal) or
`zcode-team-api-key` (`keyType=2`) location with
`usageScene:1`, then call `POST .../api_keys/{id}/access_tokens` with Bearer
business auth and `{clientType:"zcode",clientVersion:<trusted version>}`.
Customer lookup uses bare business auth; personal Z.ai key management uses
Bearer and BigModel uses bare auth; team key management uses bare auth plus
scope headers. No `/copy` request or `<apiKey>.<secretKey>` fallback remains.

A PAT is cached only in memory by credential owner, login generation, scope,
proxy and selected identity. Issuance requires a finite future absolute
`expiresAt`; a relative TTL cannot replace it. When a TTL is also returned, the earlier effective deadline applies;
refresh starts two minutes plus up to one minute jitter early. Concurrent
callers share issuance; cancelling one waiter does not cancel the shared call.
Each explicit model 401 rejects that attempt's PAT, including a final retry or
a request whose body cannot be replayed. Model HTTP 403 does not rotate the PAT.
A replayable model request retries once on explicit HTTP 401;
rejection alone never issues an extra token. A late rejection cannot invalidate
a newer PAT. Transient refresh
failures can retain an unexpired PAT for a five-second retry; disabled, malformed,
auth-rejected or expired material never falls back to a long key. A 401 received
during refresh removes that token from the transient-failure fallback too. Inference,
probes, model discovery and off-peak admission use the original owner.
Ticket status and detached idle settlement resolve PATs again from that owner,
with the same one-time explicit-401 retry; PAT rotation alone does not acquire
another ticket.
Personal/team coding-plan quota monitoring also uses this in-memory PAT manager.
It calls the fixed estate's `/api/monitor/usage/quota/limit` with raw PAT
authentication (without `Bearer`); team requests add `?type=2` and the PAT's
organization/project headers. One explicit 401 rejects and replaces that PAT
once. Quota queries neither require nor persist a long `api_key`, and malformed
or unrecognized quota responses never replace a valid usage snapshot.
Quota query flights, monitor caches and persisted scheduling windows are bound to
the credential/proxy generation. Late old-login responses cannot overwrite a
new link's windows; legacy unbound data does not pause scheduling until probed
again. PAT rotation retains its login owner's binding. See
[quota snapshot ownership](DOMESTIC_OAUTH.md).
API-key quota discovery accepts only HTTPS `open.bigmodel.cn` or `api.z.ai`, with
an omitted port or port 443. Userinfo, HTTP, other ports, lookalike names and
provider text in a relay path/query do not qualify; unknown origins never default
to the domestic monitor with the relay's credential.
Project-token control-plane 401/403 failures retain the exact rejected login
snapshot and permit account failover. Missing login material also invalidates
only its owner. Rate limits, network failures, malformed PAT responses and shared
service failures stop that provider attempt without quarantining valid accounts. Final model authentication removes every case variant of the
old auth/plan-key headers and installs the owner's PAT and, for off-peak, JWT.
Team organization/project headers are installed from the resolved PAT scope at
that same boundary, independently of generic header overrides. Personal plans
omit those headers, even though their PAT location also contains organization
and project IDs; off-peak ticket calls follow the same rule.
Linked inference, probes and discovery disable HTTP redirects, keeping all
credentials on the validated destination, including off-peak plan-key headers.
Personal/team coding-plan model discovery uses the owning estate's fixed coding
`/models` endpoint and resolves its PAT at send time. Start-plan and off-peak
accounts do not expose this coding-plan discovery endpoint. Public model caches
include the captured login owner and selected identity. Admin capability writes
compare the full request owner atomically; relinking cannot receive the previous
project's metadata, while PAT rotation within that login remains usable.

Z.ai exchanges its original OAuth token through `POST /api/auth/z/login` for
business auth before issuance. One explicit business-login 401 may repeat that
exchange using the captured original OAuth token. BigModel uses its original
login directly.
Persistent credentials retain original login and project location in
`zhipu_project_key_id`, never PATs or a public Key ID in the model `api_key` field.
Migration 293 removes old linked-account project secrets and plan-key overrides;
manual API-key accounts and start-plan ZCode JWTs are preserved. Older Z.ai links
without original OAuth material work only while their stored business token is
valid and then require reauthorization. See the
[official agent baseline](../OFFICIAL_AGENT_BASELINES.md) for the source commit
and the explicit unreleased product `3.16.2` baseline.

Accounts created by the link use `api_protocol=adaptive` with explicit
per-protocol base URLs, so neither estate ever falls back to another estate's
compiled default. Personal/team plans use their declared Chat Completions and
Anthropic endpoints. Start-plan and off-peak links expose only an Anthropic
gateway; Responses and Chat Completions requests use the existing Anthropic
adapters and retain the plan's JWT, PAT and ticket authentication. Missing linked
credentials enter account failover before model traffic or client output.
Start-plan and off-peak JWT authentication always uses `Authorization: Bearer`,
independently of the API-key account's optional auth-scheme metadata.

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
snapshot clears omitted windows and stale reset timestamps. One explicit HTTP
401 rejects the attempted PAT and permits one same-owner replacement/replay. HTTP 403 does not trigger PAT renewal. Neither may retry
with a Key ID or copied secret.

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
| Settle | `POST /api/v1/off-peak/ticket/{id}/settle` | Idempotent; empty successful 200/204 acknowledges settlement; explicit 401 refreshes PAT once, other errors retain the ticket for idle cleanup retry |

Each physical ticket-control send, including a 401 retry, carries a fresh
UUIDv4 `x-request-id`. Successful responses may be bare JSON or zero-code
`{code,data}` envelopes; nonzero business codes remain failures.
The gateway ticket-control contract evicts only the attempted PAT on 401 or 403.
Only 401 permits an immediate single same-owner retry; 403 returns its failure,
and a later independent operation may issue a new PAT. This control policy is
separate from model-call recovery and is not a claim about official-client
ticket retries.

States are `queued`, `ready`, `active`, `expired`, `settled` and `not_found`.
A model request is admitted only while the ticket is `ready` or `active`; a
`queued` request is rejected by the platform.

**Plus bounds the wait instead of queueing forever.** An off-peak credential generation holds at
most one ticket; a request rechecks a cached `ready` or `active` ticket with the
platform before reusing it, and
otherwise the server takes a ticket and polls until the platform promotes it,
within a configurable budget. When the budget expires the request fails with a
retryable status that reports the current queue position. A ticket that expires
while queued is re-taken once. Each admitted model request retains a lease through sending and response-body
EOF, Close, cancellation or read failure. Active leases prevent settlement;
the idle window starts after the final lease ends. Credential rotation admits
new work under a separate holder while older active streams drain with their
original ticket and owner. Tickets idle for longer than the settle window are
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

The preset sends `ZCode/3.16.2` together with `X-ZCode-App-Version: 3.16.2`,
`HTTP-Referer: https://zcode.z.ai`, `X-Title: Z Code@electron`,
`X-Release-Channel: production`, and `X-ZCode-Agent: glm`. Its five host
facts (`X-Client-Language`, `X-Client-Timezone`, `X-Platform`, `X-Os-Category`,
`X-Os-Version`) are generated once, persisted and editable in **System Settings
→ Outbound identity → GLM · ZCode**, with per-account overrides. Version changes
update UA and its companion together while preserving host and product facts.
No Codex `Originator` or standalone `Version` header is sent. Inference adds the pinned per-protocol AI SDK UA suffixes; control-plane
calls omit those suffixes and `X-ZCode-Agent`. Control OS version follows
`os.version()`, while inference follows `os.release()`. A persistent random
UUIDv4 device ID is shared by the selected installation scope; valid configured
account/global `X-Device-Mid` pins take precedence. Control requests send it in
`X-Device-Mid`; Anthropic inference uses the same ID in JSON-string
`metadata.user_id`, with empty `account_uuid` and the owner-scoped session ID.
Ordinary inference does not gain a device header. See [Outbound identity](../OUTBOUND_IDENTITY.md)
for exact defaults, source evidence and the source-priority matrix.

The account-link handshake and the credential-derivation calls are pre-account
authorization operations. The session retains one native identity across start,
poll, exchange and credential derivation, including Redis round trips. The
created account pins that identity. Runtime PAT requests reuse the same
credential-owner identity, and their body client version stays coherent with
its trusted declarations. Short-lived authentication rotation does not select
an identity source or alter the SDK fingerprint. Both OpenAI-compatible and
Anthropic forwarding apply the current plan credential before sending.

Model calls generate a request UUIDv4 for every physical attempt and retain one
trace UUIDv4 across retries of the logical request. Session/query attribution is
generated only when the accepted request supplies that context, within tenant
and credential-owner boundaries. The gateway uses session type `other` because
it cannot infer a caller's agent execution role. Foreign Claude/Codex runtime
headers and caller-supplied Anthropic device metadata are replaced. Persistent
ID-store failures stop the request before upstream transmission.
Each new account link persists an internal `oauth_login_id`. Project-token
renewal retains it; relinking the same row or replacing an API key uses another
session namespace. Legacy grants keep their account-row scope until relinking.
This internal login ID is never sent upstream.

## Known limitations

- **No refresh.** ZCode ships no refresh-token exchange, so an expired link must
  be re-authorized. Plus stores the refresh token when the platform supplies one
  but never rotates it, and registers no token refresher for these accounts.
- **Project locations can be recovered.** An explicit project-token 404 triggers
  bounded key-location rediscovery. Permission, disabled-token and transport
  failures cannot trigger key creation or fall back to a copied key.
- **Native plan protocols are explicit.** Start/off-peak expose only the
  Anthropic channel; Chat/Responses use the gateway's existing Anthropic adapters,
  retaining JWT/PAT/ticket authentication rather than selecting another estate.
- **Public metadata is scoped.** Persisted project/Key IDs do not replace a valid
  login, and an inaccessible project cannot recover through an old copied key.
- **Team plans need their scope.** A team subscription can own several projects,
  so the organization and project must be supplied when linking.

## Unaffected areas

Manual API-key endpoint/authentication choices, pay-as-you-go balance probes,
the Chat Completions / Responses / Anthropic adapters, `anthropic-version`, beta
handling, billing, scheduling, proxying and ingress audit retain their existing
contracts. PAT resolution does not move model dispatch ahead of ingress audit.
Count-token requests for GLM remain a local estimate and send no upstream request.

## Protocol source evidence

The PAT migration was checked against supplied ZCode commit
`3c5b227649371507ddc4eb46ac5133afe6db1cab` on 2026-10-25:
`packages/shared/src/project-access-token-client.ts`,
`project-access-token.ts`, `project-access-token-response.ts`, and
`docs/model-provider-project-access-token.md`. Selected identity/SDK defaults remain the declarations in
[Outbound identity](../OUTBOUND_IDENTITY.md) and the
[official baseline index](../OFFICIAL_AGENT_BASELINES.md). Protocol review does
not implicitly update those versions.

Requirement-based tests cover the actual issuance path and headers, absence of
Copy Secret, personal/team estates, expiry and rejection, concurrent refresh,
owner changes, cancellation and unchanged manual-key behavior. These use mock
transports; real-account PAT issuance, long off-peak queues and provider
acceptance require separate authorized upstream evidence.
