# MiniMax Code

MiniMax Code supports two backend account modes: OAuth and API Key. Client connections always use an API key created in this panel; the backend mode only affects upstream scheduling.

## Common settings

| Item | Value |
| --- | --- |
| Base URL | `{{baseUrl}}` |
| API Key | Create one on the [Keys page]({{keysUrl}}) |
| Model | `YOUR_MODEL_ID` |
| Protocol | Anthropic / Claude-compatible (per group) |

## OAuth mode

Authorize MiniMax OAuth under Accounts in the admin panel. The client still uses the base URL and API key above.

## API Key mode

Add a MiniMax API Key account in the admin panel. Client configuration is identical to OAuth mode; only the account type differs.

## Verify

Request the model list or send a short message. A 401 usually means the client is using a MiniMax key instead of the panel key.

## Notes

- Do not paste the OAuth account token into the client.
- Model IDs come from the group and the Keys page.
