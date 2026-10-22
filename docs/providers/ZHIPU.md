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

A linked subscription is not a single credential. The official client derives a
different one per plan, and the account material below mirrors it exactly. The
plan kind is chosen by the operator because it cannot be inferred from the
authorization alone.

| Plan kind | Inference credential | Protocol | Endpoint |
| --- | --- | --- | --- |
| `individual-coding-plan` | short-lived project access token (PAT) | Chat Completions / Anthropic | estate coding + Anthropic origins |
| `team-coding-plan` | short-lived team project PAT (`keyType=2` location) plus `bigmodel-organization` / `bigmodel-project` | Chat Completions / Anthropic | estate coding + Anthropic origins |
| `start-plan` | the ZCode token as a bearer credential | Anthropic | `https://zcode.z.ai/api/v1/zcode-plan/anthropic` |
| `off-peak` | the ZCode token as a bearer credential plus the project PAT | Anthropic | `https://zcode.z.ai/api/v1/off-peak/anthropic` |

Linked coding accounts resolve the default-named visible organization/project
(or explicit team scope), find/create the official key location with
`usageScene:1`, then call `POST .../api_keys/{id}/access_tokens` with Bearer
business auth and `{clientType:"zcode",clientVersion:<trusted version>}`.
Customer lookup uses bare business auth; personal Z.ai key management uses
Bearer and BigModel uses bare auth; team key management uses bare auth plus
scope headers. No `/copy` request or `<apiKey>.<secretKey>` fallback remains.

A PAT is cached only in memory by credential owner, login generation, scope,
proxy and selected identity. Expiry is the earlier of the two official lifetime
fields; refresh starts two minutes plus up to one minute jitter early. Concurrent
callers share issuance; cancelling one waiter does not cancel the shared call.
Each explicit model 401 rejects that attempt's PAT, including a final retry or
a request whose body cannot be replayed. A replayable request retries once;
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
accounts do not expose this coding-plan discovery endpoint.

Z.ai exchanges its original OAuth token through `POST /api/auth/z/login` for
business auth before issuance. BigModel uses its original login directly.
Persistent credentials retain original login and project location, never PATs.
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

## Off-peak idle plan

Off-peak is not a static credential: model traffic is admitted by a **ticket**
issued by the platform, and the official client's queue is unbounded.

| Operation | Method and path | Notes |
| --- | --- | --- |
| Availability | `GET /api/v1/off-peak/ticket/availability` | Take-number quota snapshot; gates new work only |
| Take | `POST /api/v1/off-peak/ticket` | Returns `ticket_id`, `state`, queue `position`, `next_poll_after` |
| Status | `POST /api/v1/off-peak/ticket/status` | Batch, up to 100 tickets |
| Settle | `POST /api/v1/off-peak/ticket/{id}/settle` | Idempotent; explicit 401 refreshes PAT once, other errors remain retryable by idle cleanup |

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
run's queue position. Ticket settlement retains the acquiring credentials,
identity and proxy even if settings change. A rotated grant/plan/proxy retires
the old ticket before acquiring a new one. Every ticket generation is eligible
for idle settlement. Forwarding and account probes require successful admission
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
created account pins that identity, and both OpenAI-compatible and Anthropic
forwarding use the derived plan credential.

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
- **Team plans need their scope.** A team subscription can own several projects,
  so the organization and project must be supplied when linking.

## Unaffected areas

Endpoint selection for API-key accounts, pay-as-you-go balance probes and the
ingress security-audit boundary keep their existing behavior. Count-token
requests for GLM remain a local estimate and send no upstream request.
