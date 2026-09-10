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
