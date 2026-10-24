# Grok / xAI

Sub2API Plus supports Grok OAuth subscription accounts and standard xAI API-key
accounts. Both account types expose OpenAI-compatible traffic through the
gateway.

Manage the UA and paired client declarations in **System Settings → Outbound
identity**. Native OAuth and API-key accounts retain the Grok family; only compatible
upstream accounts can choose another permitted preset. Forwarding, quota probes and Realtime handshakes consume the
same trusted account identity. See [outbound identity](../OUTBOUND_IDENTITY.md).

The HTTP/TLS transport adds the subscription proxy's `X-XAI-Token-Auth` hint
without selecting a client identity from the hostname. CLI proxy sampling and
media-mutation requests also send
`x-authenticateresponse: authenticate-response`; concrete official billing and
model-control paths omit it. The final send boundary derives both declarations
from the destination and request path, and removes them on other hosts,
including redirects. CLI rejection responses do not trigger an automatic replay
to the public API.
Compatible accounts using a Codex or other preset retain that selection on
all supported hosts.

The official protocol baseline is local `grok-build@2bdd1d6a` (client `1.0.45`).
The compiled sampler identity follows that source:
`grok-shell/1.0.45 (<os>; <arch>)`, identifier `grok-shell`, client version
`1.0.45`, mode `headless` on CLI/control requests. Official public API
requests omit the CLI mode declaration. `1.0.41` remains the accepted version floor, and a
valid credential-owning account, configured global preset or environment pin
still outranks the compiled default.

Inference requests also carry the official request-owned
sampler declarations: a UUIDv4 request ID retained for the logical prompt and
its retries, a persistent UUIDv5 agent ID, the final model, and (when an
authoritative tenant-isolated association exists) independent conversation/session
IDs plus the official UUIDv5 root-group ID. New gateway session mappings use
UUIDv7; resumed UUIDv4 sessions and official UUIDv4 auxiliary labels preserve
their type. Stored mappings include the UUID version in their scope so the same
raw value in a resumed session and a new cache-seeded session cannot conflict.
The corrected mapping namespace creates a new mapping on its first use after
upgrade; drain older workers so sessions do not alternate between namespaces.
Mappings are persisted atomically, scoped by tenant and credential
owner, and fail closed when durable mapping is unavailable. The installation
uses a persisted virtual-device seed for the UUIDv5 agent identity, avoiding raw
host machine information. This agent identity survives restarts and is distinct
from the official process-scoped UUIDv4 agent-instance identity. OAuth
requests include the credential owner's `sub` as `x-grok-user-id`. The gateway
does not fabricate a turn index, retry count, deployment ID, or tracing lineage
it does not own, and generic account header overrides cannot replace any of
these declarations. Model, billing, and media-status lookups omit mutation-only
sampler and response-authentication fields.

One constructed sampler request keeps its request association across transport
retries and redirects. A new logical
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

URL construction and ordinary relay headers use the same acquired credential
snapshot as Authorization, including Chat fallbacks, media, voice and realtime.
Resource accounting stays attached to the selected account. A configured proxy
without its captured relation stops preparation before an upstream send.

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
a single-flight guard bound to target, owner, credential generation and proxy
configuration (including proxy status); positive and negative entries cannot
cross a relink or credential rotation. There is
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
a cross-origin redirect — the gateway
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

- Signed video CDN downloads. The official download client has no default
  headers. The gateway retains its selected trusted identity for these requests
  under the repository-wide identity contract, with no bearer credential or
  account override sent to the signed CDN URL.
- TLS fingerprint. The gateway uses Go TLS rather than native Rustls/aws-lc.
  Header/protocol alignment does not make their ClientHello bytes identical.
  Grok's Go HTTP/2 profile uses the official 15-second idle PING trigger,
  five-second PING timeout, two idle connections per host, 90-second pool idle
  expiry and ten-second connect timeout. Go's health PING is idle-triggered;
  native reqwest can also send keepalive while idle. Other profiles keep their
  existing transport behavior.
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
- Claude-compatible local token counting: `/v1/messages/count_tokens`
- Standalone search (Grok groups only): `/x_search` (native `x_search`) and
  `/web_search`
- Voice (Grok groups only): `/tts`, `/stt`, custom voices, and `/realtime`
- Image generation and editing
- Video generation, editing, extension, and status lookup
- Client-facing Responses WebSocket ingress bridged to the xAI HTTP/SSE
  upstream

Local token counting enters the same Messages Content Moderation and Prompt
Audit gates after authentication and basic validation, before estimation. It
does not select a provider account. Unknown valid blocks pass through while
recognized sibling content remains auditable under the shared
[content coverage contract](../SECURITY_AUDIT_CONTENT_COVERAGE.md).

When an API-key account is marked as not supporting Responses, the Messages
adapter sends Chat Completions with that account's Grok API key and validated
Grok base URL. An empty result from another provider's key getter must never
quarantine a valid Grok grant.

The optional SSO import converts an operator-supplied Grok Web SSO cookie into
Build OAuth credentials through the trusted xAI device flow. The raw cookie is
used only for that conversion and is not persisted in account credentials.
General browser automation and web scraping remain outside this provider
integration.

## Cache and tool declarations

Responses sends `prompt_cache_key` in the JSON body: an explicit value wins,
otherwise the conversation ID is the official fallback. Compatibility-client
session signals and anchored prefix derivation are gateway fallbacks only.
The gateway namespaces cache values by tenant API key and model. Conversation,
session and root-group associations are resolved independently and tenant-isolated.
An explicit cache seed can establish a conversation fallback; a content-derived
cache key cannot. Without an explicit conversation/session/cache declaration,
the execution session is a fresh UUIDv7 retained only for that logical request,
its retries and auxiliary prompts. Independent identical prompts and subsequent
unlabelled WS turns receive different sessions while retaining cache reuse.
An auxiliary request can therefore use a new conversation ID while sharing the
parent's session, cache key and root group; descendants sharing an explicit
conversation-group declaration retain that grouping.

Cache routing never adds `web_search` or `x_search`, changes a function tool into
a hosted tool based on its name, or changes `tool_choice` to obtain cache hits.
When a request explicitly includes a hosted search tool and a same-named client
function, the hosted declaration wins, following the official Responses mapper.
Malformed or unsupported controls still follow the existing protocol validation.
The retired account tool-cache switch and client-fingerprint route are removed;
migration 280 removes only its Grok account extra key, preserving other data.
Free OAuth prompt-cache hits are upstream capability, not guaranteed by a key.

`X-Grok-Client-Tool-Cache` has no built-in meaning. It is not generated or
implicitly copied from ingress. An operator-configured extra header with that
name uses the same validation and forwarding as other extra headers, including
signed-request preservation and the project-token policy. Account overrides
cannot replace identity or request-owned declarations.

## Endpoint identity

The selected Grok snapshot declares both official client families: sampler and
control operations use `grok-shell/<version> (linux; x86_64)`; Imagine image/video
operations use `xai-grok-build/<version>` with the same selected version and
`grok-shell` client identifier. The system settings show the media declaration
separately. Native OAuth and API-key accounts both retain grok-build. Only Grok
`upstream` accounts may select another permitted preset. Inbound headers and the shared transport cannot select a family.
Media start/poll requests carry an isolated `x-grok-session-id` when available;
they do not acquire sampler request/model/agent fields.

## Outbound privacy and local failures

Routing Host/HTTP2 authority may contain `sub2api`; URL safety policy remains
in force. Other outbound headers/trailers retain the case-insensitive project
identifier prohibition. Sensitive or already-signed prohibited declarations
fail locally before network dispatch. These failures are recorded as platform /
gateway, stage `outbound_policy`, with a stable reason and no credential values;
they do not create provider cooldowns or account failover.

## Account Types

OAuth accounts use the xAI subscription authorization flow and subscription
proxy. API-key accounts use `https://api.x.ai/v1` by default. Administrators
create either account type from the dashboard and attach it to a Grok group.
The global Grok CLI/API mode controls only an OAuth account with no explicit
`base_url`; an API-key account with no explicit URL always remains on the public
API.

## OAuth Configuration

The OAuth authorization-code flow uses PKCE and fresh UUIDv7 state/nonce. The
server retains the nonce, issuer, client ID and outbound identity snapshot with
the single-use authorization session. Before returning a user identity it
validates the ID token signature, permitted asymmetric algorithm, issuer,
audience, expiry, subject and nonce against the official discovery/JWKS origin.
The official team-principal exception uses only the access token returned by
the trusted token exchange. Token exchange and refresh redirects must retain
the configured endpoint's scheme, host and port, with the standard ten-hop
limit. Cross-origin redirects and HTTPS downgrades stop before sending the
code, verifier or refresh token to the next destination. Explicit operator URL
overrides remain the initial trusted endpoint; they cannot delegate trust by
redirect. Permitted redirects retain the operation's identity snapshot.
Refresh grants do not require a new login nonce.
Authorization sessions started before nonce binding was installed must restart.
Default public client values can be overridden:

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

xAI usage combines the official billing summary with supported upstream
rate-limit observations. A manual quota probe can collect fresh headers from
the sampler; billing refresh uses the official billing endpoint. Neither path
invents subscription quota values. Before a usable upstream observation, quota
remains unknown while local usage is still shown.

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

The [official grok-build baseline](../OFFICIAL_AGENT_BASELINES.md) remains
`1.0.45`. Public `api.x.ai` HTTP and realtime WebSocket forwarding/probes omit
`x-grok-client-mode` and CLI-proxy authentication declarations; CLI proxy/control
calls retain their official destination-specific declarations. Migration 291
removes obsolete Grok API-key compatible-family selections/defaults atomically.

### Credential-owned observations

Billing and response quota snapshots carry an opaque credential/proxy generation.
Probe coalescing uses that generation; conditional persistence, cooldowns and
recovery compare the exact sent credentials before changing account state.
Their owner evidence includes the hydrated proxy endpoint, credentials and
status, compared under a proxy-row share lock in the same atomic mutation.
Missing hydration never bypasses the comparison. A proxy-invalid failure must
explicitly identify the missing configured proxy; quarantine locks its deleted
row and rejects a concurrent restoration. A same-row route or status repair
also invalidates old generation-bound observations and runtime blocks.
Manual connection tests and derived auto-pause thresholds use the same fence.
Slim scheduler records retain only the grant digest needed to check these
observations; they cannot act as credential snapshots for state mutations.
A late response cannot authorize media or install scheduling state for a new
login. Reads ignore legacy unbound or mismatched observations until a fresh
probe succeeds. Explicit administrator media overrides retain precedence.
Normal refresh preserves session mapping while invalidating obsolete credential
observations; a fresh token does not inherit an older token’s late error.
Replacing an API key changes its session mapping; OAuth session ownership is
bound to the authenticated subject/team so ordinary token rotation preserves it.
