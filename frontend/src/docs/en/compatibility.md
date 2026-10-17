# Compatible clients

The following clients do not have their own outbound identity. They map to Codex or the OpenAI-compatible entry point by default. They still work; the admin panel simply shows them as Codex.

## OpenCode

The recommended path is "Use key" on the Keys page: choose OpenCode and copy the generated `opencode.json`. Minimal manual configuration:

```json
{
  "$schema": "https://opencode.ai/config.json",
  "provider": {
    "sub2api": {
      "npm": "@ai-sdk/openai-compatible",
      "name": "Sub2API Plus",
      "options": {
        "baseURL": "{{openaiBaseUrl}}",
        "apiKey": "<YOUR_API_KEY>"
      },
      "models": {
        "YOUR_MODEL_ID": { "name": "YOUR_MODEL_ID" }
      }
    }
  }
}
```

## Cline / Command Code

Choose OpenAI Compatible in the provider settings:

- Base URL: `{{openaiBaseUrl}}`
- API Key: panel key
- Model: `YOUR_MODEL_ID`

## TypeSafe / System One

TypeSafe / System One uses a native endpoint, not the OpenAI-compatible path. Select a TypeSafe group on the Keys page and use "Use key" to copy the curl configuration.

## Notes

- A compatible client changes only the inbound connection; it does not change the outbound identity.
- If the client already requires `/v1`, do not add another one.
