# Kimi Code

Kimi Code uses the Anthropic / Claude-compatible entry point. The "Kimi Code" outbound identity is for upstream requests and does not change the address entered in the client.

## Settings

| Item | Value |
| --- | --- |
| Base URL | `{{baseUrl}}` |
| API Key | Create one on the [Keys page]({{keysUrl}}) |
| Model | `YOUR_MODEL_ID` |
| Protocol | Anthropic / Claude-compatible (per group) |

## If the client supports environment variables

```bash os=unix
export ANTHROPIC_BASE_URL="{{baseUrl}}"
export ANTHROPIC_AUTH_TOKEN="<YOUR_API_KEY>"
```

```bat os=cmd
set ANTHROPIC_BASE_URL={{baseUrl}}
set ANTHROPIC_AUTH_TOKEN=<YOUR_API_KEY>
```

```powershell os=powershell
$env:ANTHROPIC_BASE_URL="{{baseUrl}}"
$env:ANTHROPIC_AUTH_TOKEN="<YOUR_API_KEY>"
```

## If the client only supports a custom provider

Choose an Anthropic / Claude-compatible provider and enter the base URL, API key, and model. Do not add `X-Msh-*` device headers manually; the gateway renders them from the outbound identity settings.

## Notes

- Kimi Code model IDs and membership permissions vary; use the group and Keys page as the source of truth.
- Device declarations, language, and timezone are configured under Outbound identity → Kimi Code.
