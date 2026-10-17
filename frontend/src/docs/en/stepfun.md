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

## Notes

- Step-Code is a versionless client family; no version is required in the admin panel.
- Model IDs come from the group and the Keys page.
