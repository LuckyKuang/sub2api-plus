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
With Redis configured the session is shared across replicas; without it the
session is process-local, so a multi-instance deployment should either configure
Redis or keep the flow on one instance.

## Plan kinds

A linked subscription is not a single credential. The official client derives a
different one per plan, and the account material below mirrors it exactly. The
plan kind is chosen by the operator because it cannot be inferred from the
authorization alone.

| Plan kind | Inference credential | Protocol | Endpoint |
| --- | --- | --- | --- |
| `individual-coding-plan` | derived project API key (`<apiKey>.<secretKey>`) | Chat Completions / Anthropic | estate coding + Anthropic origins |
| `team-coding-plan` | derived team project key (`keyType=2`) plus `bigmodel-organization` / `bigmodel-project` | Chat Completions / Anthropic | estate coding + Anthropic origins |
| `start-plan` | the ZCode token as a bearer credential | Anthropic | `https://zcode.z.ai/api/v1/zcode-plan/anthropic` |
| `off-peak` | the ZCode token as a bearer credential plus the derived project key | Anthropic | `https://zcode.z.ai/api/v1/off-peak/anthropic` |

Deriving a project key follows the published sequence: read the customer profile,
select a non-team organization/project (the default-named pair is preferred, and
`projectType == "2"` team projects are skipped for personal keys), find or create
the published key name, then copy its secret. Z.ai requires the copied secret;
BigModel historically accepted the bare key.

On the Z.ai estate the handshake returns an **OAuth** token that must first be
exchanged through `POST /api/auth/z/login` for a business token; every business
and key-derivation call uses the business token. BigModel already returns a
business token and needs no exchange.

Accounts created by the link use `api_protocol=adaptive` with explicit
per-protocol base URLs, so neither estate ever falls back to another estate's
compiled default.

## Off-peak idle plan

Off-peak is not a static credential: model traffic is admitted by a **ticket**
issued by the platform, and the official client's queue is unbounded.

| Operation | Method and path | Notes |
| --- | --- | --- |
| Availability | `GET /api/v1/off-peak/ticket/availability` | Take-number quota snapshot; gates new work only |
| Take | `POST /api/v1/off-peak/ticket` | Returns `ticket_id`, `state`, queue `position`, `next_poll_after` |
| Status | `POST /api/v1/off-peak/ticket/status` | Batch, up to 100 tickets |
| Settle | `POST /api/v1/off-peak/ticket/{id}/settle` | Idempotent; 4xx is treated as an acknowledgement |

States are `queued`, `ready`, `active`, `expired`, `settled` and `not_found`.
A model request is admitted only while the ticket is `ready` or `active`; a
`queued` request is rejected by the platform.

**Plus bounds the wait instead of queueing forever.** An off-peak account holds at
most one ticket; a request that finds it `ready` or `active` reuses it, and
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
| `3102` / `3001` | ticket unusable on the model endpoint | the ticket is discarded and re-taken |
| `3105` | queue valve on the model endpoint | `429`, with `Retry-After` |

Two documented deviations from the official client remain. The official client
binds a ticket to a whole run segment (every turn and retry) and settles it when
the segment ends; a stateless gateway has no segment boundary, so Plus reuses a
ticket across requests until it goes idle and then settles it. And the official
client waits forever while queued, re-entering the queue without consuming its
retry budget; Plus fails fast after its budget so a user request cannot occupy a
gateway slot indefinitely.

**Not supported:** the off-peak `task_id` continuation semantics. Plus issues its
own task id per account and never resumes a previous run's queue position.

## Outbound identity

Zhipu GLM accounts advertise the pinned ZCode client identity by default, for
both API-key and linked accounts. ZCode is the provider's own official client and
renders its product, version companion and runtime declaration block.
OAuth accounts are pinned to that family; an API-key account can still select
another preset.

The preset sends `ZCode/3.14.3` together with `X-ZCode-App-Version: 3.14.3`,
`HTTP-Referer: https://zcode.z.ai`, `X-Title: Z Code@electron`,
`X-Release-Channel: production`, and `X-ZCode-Agent: glm`. Its five host
facts (`X-Client-Language`, `X-Client-Timezone`, `X-Platform`, `X-Os-Category`,
`X-Os-Version`) are generated once, persisted and editable in **System Settings
→ Outbound identity → GLM · ZCode**, with per-account overrides. Version changes
update UA and its companion together while preserving host and product facts.
No Codex `Originator` or standalone `Version` header is sent. The existing SDK
fingerprint stays unchanged. See [Outbound identity](../OUTBOUND_IDENTITY.md)
for exact defaults, source evidence and the source-priority matrix.

The account-link handshake and the credential-derivation calls are pre-account
authorization operations, so they carry the same native Zhipu identity.

## Known limitations

- **Start-plan and off-peak are Anthropic-protocol only.** Their endpoints exist
  only under the ZCode plan gateway, so a Chat Completions request routed to such
  an account falls back to the estate's default Chat Completions origin and will
  not be admitted. The official client also uses `anthropic-messages` for both.
- **No refresh.** ZCode ships no refresh-token exchange, so an expired link must
  be re-authorized. Plus stores the refresh token when the platform supplies one
  but never rotates it, and registers no token refresher for these accounts.
- **Resolution is per request.** A coding-plan key is derived at link time. If the
  provider rotates the project key, re-link the account.
- **Team plans need their scope.** A team subscription can own several projects,
  so the organization and project must be supplied when linking.

## Unaffected areas

Endpoint selection for API-key accounts, the Chat Completions / Responses /
Anthropic adapters, `x-api-key` or bearer authentication, `anthropic-version`,
beta handling, model discovery, coding-plan quota and pay-as-you-go balance
probes, billing, scheduling, proxying and the ingress security-audit boundary all
keep their existing behavior. Count-token requests for GLM remain a local
estimate and send no upstream request.
