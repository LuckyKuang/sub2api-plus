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

Inference and media-mutation requests also carry the official request-owned
sampler declarations: a new request ID, a process-stable random agent ID, the
final model, and (when a stable tenant-isolated conversation exists) matching
conversation/session IDs plus the official UUIDv5 conversation-group ID. OAuth
requests include the credential owner's `sub` as `x-grok-user-id`. The gateway
does not fabricate a turn index, retry count, deployment ID, or tracing lineage
it does not own, and generic account header overrides cannot replace any of
these declarations. Model, billing, and media-status lookups omit mutation-only
sampler and response-authentication fields.

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
