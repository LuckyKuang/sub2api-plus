# Grok / xAI

Sub2API Plus supports Grok OAuth subscription accounts and standard xAI API-key
accounts. Both account types expose OpenAI-compatible traffic through the
gateway.

Manage the UA and paired client declarations in **System Settings → Outbound
identity**. Native OAuth retains the Grok family; API-key accounts can choose
another preset. Forwarding, quota probes and Realtime handshakes consume the
same trusted account identity. See [outbound identity](../OUTBOUND_IDENTITY.md).

The HTTP/TLS transport adds the subscription proxy's `X-XAI-Token-Auth` hint
without selecting a client identity from the hostname. CLI proxy sampling and
media-mutation requests also send
`x-authenticateresponse: authenticate-response`; concrete official billing and
model-control paths omit it. The final send boundary derives both declarations
from the destination and request path, and removes them on other hosts,
including redirects and the narrowly matched CLI-to-public API compatibility
fallback. The fallback keeps the selected UA and companion identity headers.
Compatible accounts using a Codex or other preset retain that selection on
both hosts.

The compiled default identity follows the frozen local grok-build source:
`grok-shell/1.0.45 (<os>; <arch>)`, identifier `grok-shell`, client version
`1.0.45`, mode `headless`. `1.0.41` remains the accepted version floor, and a
valid credential-owning account, configured global preset or environment pin
still outranks the compiled default.

Inference and media-mutation requests also carry the official request-owned
sampler declarations: a new request ID, a process-stable random agent ID, the
final model, and (when a stable tenant-isolated conversation exists) matching
conversation/session IDs plus the official UUIDv5 conversation-group ID. OAuth
requests include the credential owner's `sub` as `x-grok-user-id`. The gateway
does not fabricate a turn index, retry count, deployment ID, or tracing lineage
it does not own, and generic account header overrides cannot replace any of
these declarations. Model, billing, and media-status lookups omit mutation-only
sampler and response-authentication fields.

One constructed sampler request keeps its request association across transport
retries, redirects and the `api.x.ai` compatibility fallback. A new logical
sampler call (including a rebuilt resubmit body) renders a fresh association, so
the upstream sees transport replay and agent resubmit as different things.

Sampler `Accept` follows the operation actually sent rather than the downstream
client's format: the plain JSON operation declares `application/json`, and a
request whose final body streams upstream declares `text/event-stream` — which
includes the case where a non-streaming downstream client is served by upstream
SSE aggregation. `Content-Type` stays `application/json` for the JSON sampler
body. Account header overrides cannot change the operation-owned `Accept`.

## Request compression

The gateway can send zstd-compressed sampler JSON toward the trusted Grok CLI
proxy. It is enabled by `gateway.grok.grok_request_compression_enabled`
(environment `GATEWAY_GROK_REQUEST_COMPRESSION_ENABLED`, default `true`), where
`false` is the operator kill switch and always sends plain JSON. Enabled only
permits negotiation; it never authorizes compression by itself.

Before compressing, the gateway reads `GET {base_url}/settings` from the exact
normalized target (scheme, host, effective port and base path) — the same
`cli-chat-proxy.grok.com/v1` route the sampler uses — with the credential
owner's bearer, the subscription proxy hint and the same-owner identity
snapshot, over the account's configured proxy and the existing URL validation.
Compression happens only when the response's `accept_request_encodings`
contains the exact token `zstd`. Missing, unknown-shaped, non-success or failed
lookups, a withdrawn advertisement, another origin, another credential owner or
another proxy all keep plain JSON; a lookup failure never fails the inference
and never creates an audit decision. One probe is bounded by a five-second
timeout. The capability is cached in-process for five minutes, refreshed through
a single-flight guard bound to target, owner and proxy configuration; there is
no persistent table or background service.

The final JSON is encoded after model, tool and session adaptation, at zstd
level 3, only from 64 KiB of body upward; bodies of 1 MiB and above acquire a
bounded encode slot first. `Content-Encoding: zstd` appears only together with
the compressed bytes, and `Content-Length`, `Body` and `GetBody` always describe
the bytes actually sent. Below the threshold, on failure, or with the kill
switch off, the request is plain JSON with no stale compression declaration, and
a failure logs a fixed stage/code with byte counts and no request content.
Generic account header overrides cannot supply `Content-Encoding`.

A same-target retry may replay the encoded body. When the destination changes —
the `api.x.ai` compatibility fallback or a cross-origin redirect — the gateway
rebuilds a plain body from the final JSON, drops the declaration and keeps the
same-owner selected identity; if the body cannot be rebuilt it keeps its
existing transport error semantics instead of sending a wrong encoding.
Compression never changes audit, usage payload hashing, inflight estimation,
cache/session keys or billing, so a compressed request still produces exactly
one inference and one billing record. Response decompression is independent of
request compression.

## Agent-only differences and follow-up scope

Native grok-build carries agent features that the gateway intentionally does not
imitate. They are recorded here instead of being approximated with surface
headers or fabricated values:

- Media tool user agent. The native media tool advertises
  `xai-grok-build/<version>` while the gateway keeps the selected account
  snapshot (for example the Grok shell family). Unifying the media-tool family
  is separate work; a Plus media request never reselects identity.
- Transport fingerprint. Native uses Rustls with aws-lc and its own HTTP/2 ping,
  idle and connect timings. The gateway keeps its existing TLS/HTTP2 profile and
  fingerprint. Moving to the Rust parameters is a separately described change
  with its own regression evidence, not part of a version update.
- Doom-loop recovery. The sampler's doom-loop and exact-repetition control
  headers only make sense with the matching event interception and resampling
  policy. The gateway sends neither the headers nor fabricated values.
- Enterprise deployment authorization. Native enterprise deployment auth
  (`x-grok-deployment-id` and its credential flow) requires authoritative
  deployment state the gateway does not own, so no deployment declaration is
  emitted.
- Turn, resubmit and tracing declarations. `x-grok-turn-idx`,
  `x-grok-transient-retry` and tracing lineage require the agent's authoritative
  turn/resubmit state. The gateway omits them rather than inventing a fixed turn
  index, retry counter or `traceparent`.

## Supported Interfaces

- Responses: `/v1/responses`, `/responses`, `/backend-api/codex/responses`
- Chat Completions: `/v1/chat/completions`, `/chat/completions`
- Claude-compatible Messages: `/v1/messages`
- Standalone search (Grok groups only): `/x_search` (native `x_search`) and
  `/web_search`
- Voice (Grok groups only): `/tts`, `/stt`, custom voices, and `/realtime`
- Image generation and editing
- Video generation, editing, extension, and status lookup
- Client-facing Responses WebSocket ingress bridged to the xAI HTTP/SSE
  upstream

The optional SSO import converts an operator-supplied Grok Web SSO cookie into
Build OAuth credentials through the trusted xAI device flow. The raw cookie is
used only for that conversion and is not persisted in account credentials.
General browser automation and web scraping remain outside this provider
integration.

## Request-Level Tool Cache Preference

Custom clients and integrations may send
`X-Grok-Client-Tool-Cache: prefer-cache` to enable, or
`X-Grok-Client-Tool-Cache: off` to disable, the request-level tool-cache
preference where the Grok route supports it. This is a Sub2API Plus gateway
control, not an official Grok/xAI header. The gateway consumes it locally and
does not forward it upstream. The retired branded and generic gateway header
names are not recognized.

## Account Types

OAuth accounts use the xAI subscription authorization flow and subscription
proxy. API-key accounts use `https://api.x.ai/v1` by default. Administrators
create either account type from the dashboard and attach it to a Grok group.
The global Grok CLI/API mode controls only an OAuth account with no explicit
`base_url`; an API-key account with no explicit URL always remains on the public
API.

## OAuth Configuration

The OAuth flow uses PKCE. Default public client values can be overridden:

The built-in authorization request identifies `grok-build` as its referrer and
requests the current Grok Build scopes for API proxy, conversation, and
workspace access. An `XAI_OAUTH_SCOPE` override replaces that complete scope
set, so deployments should retain every permission needed by their enabled
Grok features.

| Variable | Purpose |
| --- | --- |
| `XAI_OAUTH_CLIENT_ID` | OAuth client ID |
| `XAI_OAUTH_SCOPE` | Requested scopes |
| `XAI_OAUTH_REDIRECT_URI` | Local callback URI |
| `XAI_OAUTH_AUTHORIZE_URL` | Authorization endpoint |
| `XAI_OAUTH_TOKEN_URL` | Token endpoint |
| `XAI_BASE_URL` | Runtime diagnostics base URL |
| `XAI_GROK_CLI_VERSION` | Grok preset version fallback below account/global settings; does not affect other presets or select an identity by host |

Do not commit OAuth credentials. Account credentials reuse the encrypted account
fields for access token, refresh token, expiry, base URL, email, subscription
tier, and entitlement status.

## Administrative OAuth Endpoints

| Endpoint | Purpose |
| --- | --- |
| `POST /api/v1/admin/grok/oauth/auth-url` | Generate an authorization URL |
| `POST /api/v1/admin/grok/oauth/exchange-code` | Exchange a callback or code |
| `POST /api/v1/admin/grok/oauth/refresh-token` | Validate or refresh a token |
| `POST /api/v1/admin/grok/accounts/:id/refresh` | Refresh an account |

## CLI Configuration

Create a Grok group and a Sub2API Plus API key assigned to it. The dashboard's
**Use Key** action generates platform-specific Grok CLI and OpenCode
configuration. When configuring manually, the public `base_url` is the
Sub2API Plus URL ending in `/v1`, not the internal xAI proxy URL.

Keep generated API keys private and back up an existing CLI configuration
before replacing it.

## Quotas and Media Eligibility

xAI quota display is passive: Sub2API Plus records supported upstream
rate-limit headers but does not invent subscription quota values. Before a
usable upstream observation, quota remains unknown while local usage is still
shown.

Authentication, entitlement, and rate-limit failures temporarily affect account
scheduling according to their status. Shared model-capacity / high-demand
errors fail the current request immediately without same-account retry or
account failover. New OAuth media requests require positive
paid-entitlement evidence; API-key accounts remain eligible. Administrators can
override media eligibility with `extra.grok_media_eligible`.

An HTTP 200 billing response without authoritative quota/entitlement fields is
`billing_inconclusive` and does not authorize new OAuth image/video generation.
An explicit administrator override still wins. Clearing the override restores
automatic evaluation. This account eligibility decision is independent of
content-audit extraction: unknown valid content keeps the audit pass-through
contract.

## Models and Subscription Tiers

The catalog includes `grok-4.6` (`grok-4.6-latest` maps to it). Unregistered
Grok text models fall back to the `grok-4.5` price card.

OAuth refresh can replace a stale subscription snapshot with the JWT `tier`
when that value is more recent. Cross-client model mapping
(`grok_cross_client_model_map_enabled`) stays opt-in. Password authorization
remains disabled.
