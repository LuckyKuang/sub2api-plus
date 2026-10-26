# Grok / xAI

Sub2API Plus supports Grok OAuth subscription accounts and standard xAI API-key
accounts. Both account types expose OpenAI-compatible traffic through the
gateway.

Fallback token pricing recognizes `grok-4.7` / `grok-4.7-latest` and the separate
Fast aliases `grok-4.7-fast` / `grok-4.7-build-fast`. Per million tokens, the
standard card is $2 input, $0.50 cached input and $6 output; Fast is $4, $1 and
$12. At 200,000 prompt tokens (inclusive), standard rates double and Fast rates
multiply by 1.5. Configured channel/model prices retain their existing priority.

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
Polling preserves `authorization_pending`, increases the interval by five
seconds after every `slow_down`, and stops on denial or expiry. Verification
and consent consume the original device grant lifetime. Cancellation or an
expired grant prevents further polls and discards late successful responses;
an upstream error description cannot expose cookies, device codes or proxy
passwords. These rules follow [RFC 8628, sections 3.2 and
3.5](https://www.rfc-editor.org/rfc/rfc8628.html#section-3.5).
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

Existing-account reauthorization is independent of account creation. Code,
refresh-token and SSO reauthorization capture the initiating account and modal
generation. Replacing the account, credentials or saved proxy, closing/reopening
the modal or disposing it invalidates outstanding results. Already-issued
credential writes may finish on the server, but their late success/failure
cannot close, notify or clear loading in a replacement flow. Raw SSO cookies
and passwords are never included in the persisted reauthorization payload.

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
Default public client values remain configurable as listed below.

Personal browser login verifies the returned ID token before returning account
credentials. The authorization nonce is stored with the single-use session,
including Redis sessions served by another instance. Discovery uses the pinned
issuer `https://auth.x.ai`; the JWKS URI must stay on that issuer's origin, and
metadata requests do not follow redirects. Verification checks the asymmetric
signature, advertised algorithm, signing-key ID, issuer, audience, expiry,
issued-at time, subject, nonce, and authorized party when applicable. Missing ID
tokens, missing verifiers and failed verification reject the login. The explicit
`principal_type: Team` plus nonempty `principal_id` in a parseable JWT matches official
team consent, which omits user-only OIDC scopes; it never imports an unverified
personal email or subject.

The proxy URL selected at login start is retained for token exchange, discovery
and JWKS. A callback cannot bind or switch proxies; restart login to select
another route. A missing or invalid configured proxy, refusal, timeout or 407
never authorizes direct networking. Metadata requests use the same trusted
Grok identity without authorization codes, ID/access/refresh tokens or cookies.

Protocol source: [`xai-org/grok-build` at `2bdd1d6a`,
`xai-grok-login/src/oidc/protocol.rs`](https://github.com/xai-org/grok-build/blob/2bdd1d6a/crates/codegen/xai-grok-login/src/oidc/protocol.rs)
(`peek_access_token_principal`, `extract_user_info`,
`validate_and_extract_user_info`), plus [OIDC Core 1.0 ID-token
validation](https://openid.net/specs/openid-connect-core-1_0.html#IDTokenValidation).
Regressions: `grok_oidc_contract_test.go`, `xai/oidc_contract_test.go` and
`grok_oidc_proxy_contract_test.go`, including real proxy failures after preceding
browser-login stages through the production client.

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

Active quota probes and billing requests deep-copy the loaded credential owner
and strictly validate its captured proxy before token lookup or refresh. A
missing, mismatched or malformed binding fails without provider requests or
observation writes. The request never repairs that binding by reading a newer
proxy row. An explicitly unbound account may connect directly and ignores a
stale hydrated proxy edge. Scheduling pauses still allow diagnostics with valid
credentials and a valid route; they do not bypass these integrity checks.

The detached `/v1/models` discovery triggered by quota diagnostics captures a
deep copy of credentials and the proxy before scheduling background work. It
uses that route without rereading the proxy repository. Model observations are
published through the shared atomic credential/proxy-owner comparison; a late
response cannot update a relinked login or an edited proxy's model metadata.

Inference and Realtime requests retain the credential and proxy owner captured
before credential lookup. Quota observations, account cooldowns and configured
403 rules commit through one conditional owner-and-scheduler transaction with
the scheduler outbox. A relink, proxy edit, manual pause, newer cooldown or failed
transaction prevents publication of that request's state. Account and model
runtime blocks also retain their credential/proxy owner, including when an old
publisher resumes after reauthorization. Team limits still apply to accounts
sharing the observed team. Request observation writers reject caller-owned
transactions because they cannot publish runtime effects before the caller's
commit. Successful responses can clear only the rate limit observed by that
same owner.

The OAuth Chat-to-Responses bridge is a separate sender and captures the same
owner before token lookup. Token acquisition compares the complete route,
credential identity and client declarations at version adoption, refresh reread,
lock wait and refresh completion. A rotation issued for that captured grant
commits through the full credential/scheduler/proxy CAS before the request's
snapshot advances. A background access-token rotation may be adopted only with
the same refresh grant and identity; a replacement login is retried through
account selection. Manual diagnostics preserve their existing scheduling bypass.

Issuer failures use the same captured credential and proxy ownership. Permanent
and temporary quarantine compare the proxy endpoint, authentication and lifecycle,
including edits that retain its row ID. A still-missing proxy binding can be
quarantined, while a restored binding wins over that old failure. Quarantine and
its scheduler outbox event share the caller's transaction; scheduler cache
publication waits for commit. A failed quarantine CAS may recover only an
access-token or expiry repair with the same refresh grant and identity. A new
refresh grant, subject, team, client, base URL, identity or route requires account
selection again and cannot lend its token to the old request.

Credential recovery clears only runtime and model failure generations captured
before its successful commit. A later failure remains blocked even when it has
the same deadline. Manual reset advances a persisted recovery generation with
its outbox event; refreshed scheduler snapshots ignore older local model blocks
on every instance. Each observation binds its effects to the generation returned
by its own atomic commit. A failure committed after reset remains valid even if
its request started earlier, or the reset's local cleanup has not completed.
Local cleanup compares the captured failure sequences instead of clearing later
observations. The in-process publication epoch remains the fallback for accounts
without a saved recovery generation. True shared team limits remain separate.

Active quota probes, billing probes and account-connection diagnostics use the
same owner transaction. A stale owner or storage failure leaves the current
account's quota, billing and scheduling state intact. A received diagnostic
response can still be shown, with `persisted=false` on quota/billing results;
an upstream billing failure still returns its normal failure. A Voice HTTP 200
now observes quota headers and can recover the exact earlier rate limit in the
same transaction as its quota snapshot.

Changing the proxy or restarting an account-creation flow invalidates its old
browser generation. Late SSO, password-import and account-creation completions
cannot close, notify or start model synchronization for the replacement flow.
An account POST already sent to the server may still complete there. The
password-import control is currently hidden; its event handler retains the same
generation boundary.

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
