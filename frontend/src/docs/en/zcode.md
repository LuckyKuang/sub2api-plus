# GLM · ZCode

GLM / ZCode uses the Anthropic / Claude-compatible entry point. Do not confuse this with the "ZCode account link" in the admin panel: account linking gives the gateway an upstream credential, while client connections still use a panel API key.

## Settings

| Item | Value |
| --- | --- |
| Base URL | `{{baseUrl}}` |
| API Key | Create one on the [Keys page]({{keysUrl}}) |
| Model | `YOUR_MODEL_ID` |
| Protocol | Anthropic / Claude-compatible (per group) |

## Claude Code style

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

## Notes

- Language, timezone, and platform declarations under Outbound identity → GLM · ZCode only affect gateway-to-upstream requests.
- Model IDs come from the group and the Keys page.
- For Coding Plan, confirm the account and group are bound.
