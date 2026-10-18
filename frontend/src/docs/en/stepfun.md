# StepFun · Step-Code

StepFun / Step-Code supports Responses and Messages clients. Client connections use a panel API key.

## Settings

| Item | Value |
| --- | --- |
| OpenAI / Responses Base URL | `{{openaiBaseUrl}}` |
| Anthropic / Messages Base URL | `{{baseUrl}}` |
| API Key | Create one on the [Keys page]({{keysUrl}}) |
| Model | `YOUR_MODEL_ID` |

## Choose the protocol

- Codex, OpenCode, and other Responses clients: use `{{openaiBaseUrl}}`.
- Claude Code and other Messages clients: use `{{baseUrl}}`.
- Do not mix both protocols in one client configuration.

## Claude Code-compatible clients

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

- Step-Code is a versionless client family; no version is required in the admin panel.
- Model IDs come from the group and the Keys page.
