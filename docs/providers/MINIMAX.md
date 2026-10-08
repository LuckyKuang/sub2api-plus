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

## Coding-plan quota origin

Automatic quota requests are enabled only for coding-mode accounts whose
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
