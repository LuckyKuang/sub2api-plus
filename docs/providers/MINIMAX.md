# MiniMax

MiniMax is an API-key provider integrated with account/group selection, model
listing, platform quotas, channel monitoring and composite routing. It uses
the OpenAI-compatible gateway rather than an OpenAI OAuth credential flow.
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

MiniMax API-key accounts advertise the pinned MiniMax product identity by
default. The official MiniMax Code client renders one declaration, the bare
product token `MiniMaxAgent`, for managed provider requests and never puts its
package version on the wire, so this preset is a registered versionless client
family under [Outbound identity](../OUTBOUND_IDENTITY.md): `Version` is empty
and only the User-Agent reaches the wire.

| | Before | After |
| --- | --- | --- |
| `User-Agent` | `codex_cli_rs/0.158.0 (Ubuntu 24.04; x86_64) xterm-256color` | `MiniMaxAgent` |
| `Originator` | `codex_cli_rs` | not sent |
| `Version` | `0.158.0` | not sent |

Only the client declarations change. Endpoint selection, the Chat Completions /
Responses / Messages / adaptive protocol adapters, `x-api-key` or Bearer
authentication, `anthropic-version`, beta handling, model discovery, coding-plan
quota queries, billing, scheduling, proxying and the ingress security-audit
boundary keep their existing behavior; identity is applied at account selection
and again at the shared HTTP transport boundary. Count-token requests for
CN providers remain a local estimate and send no upstream request, and WebSocket
mode stays unavailable outside the OpenAI platform.

An account selection or the `minimax` type default can opt a MiniMax account
back into Codex or any other compatible preset. The managed MiniMax session
headers (`X-Mavis-Session-Id`, `-Agent-Id`, `-Timezone-Offset`) are request state
owned by the protocol layer and are not part of this identity, so the gateway
does not fabricate them.
