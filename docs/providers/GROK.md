# Grok / xAI

Sub2API Plus supports Grok OAuth subscription accounts and standard xAI API-key
accounts. Both account types expose OpenAI-compatible traffic through the
gateway.

## Supported Interfaces

- Responses: `/v1/responses`, `/responses`, `/backend-api/codex/responses`
- Chat Completions: `/v1/chat/completions`, `/chat/completions`
- Claude-compatible Messages: `/v1/messages`
- Image generation and editing
- Video generation, editing, extension, and status lookup
- Client-facing Responses WebSocket ingress bridged to the xAI HTTP/SSE
  upstream

TTS, transcription, browser automation, cookie handling, and web scraping are
outside this provider integration.

## Account Types

OAuth accounts use the xAI subscription authorization flow and subscription
proxy. API-key accounts use `https://api.x.ai/v1` by default. Administrators
create either account type from the dashboard and attach it to a Grok group.

## OAuth Configuration

The OAuth flow uses PKCE. Default public client values can be overridden:

| Variable | Purpose |
| --- | --- |
| `XAI_OAUTH_CLIENT_ID` | OAuth client ID |
| `XAI_OAUTH_SCOPE` | Requested scopes |
| `XAI_OAUTH_REDIRECT_URI` | Local callback URI |
| `XAI_OAUTH_AUTHORIZE_URL` | Authorization endpoint |
| `XAI_OAUTH_TOKEN_URL` | Token endpoint |
| `XAI_BASE_URL` | Runtime diagnostics base URL |
| `XAI_GROK_CLI_VERSION` | Optional client identity override |

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
scheduling according to their status. New OAuth media requests require positive
paid-entitlement evidence; API-key accounts remain eligible. Administrators can
override media eligibility with `extra.grok_media_eligible`.
