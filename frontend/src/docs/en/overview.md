# Quick start

This documentation is organized by **outbound client**. Each client speaks a specific protocol; choosing the wrong one usually shows up as a 404, a 401, or "model not found".

## 1. Create an API key

Open the [Keys page]({{keysUrl}}), create a key, and confirm it has a group assigned. The key is shown only once.

## 2. Confirm the base URL

The API address for this deployment is:

`{{baseUrl}}`

- OpenAI-compatible clients: `{{openaiBaseUrl}}`
- Anthropic / Claude Code: `{{baseUrl}}`
- Gemini native: `{{baseUrl}}`

If the administrator configured an API base URL under Site Settings, use the value shown in the panel. Do not append `/v1` twice.

## 3. Choose a model

Model IDs come from the key's group and its channels, so they are intentionally not hard-coded here. Open the [Model Plaza]({{modelPlazaUrl}}) or use "Use key" on the Keys page to see the current catalog. Replace every `YOUR_MODEL_ID` in the examples with a real model.

## 4. Send the first request

```bash
curl {{openaiBaseUrl}}/chat/completions \
  -H "Authorization: Bearer <YOUR_API_KEY>" \
  -H "Content-Type: application/json" \
  -d '{"model":"YOUR_MODEL_ID","messages":[{"role":"user","content":"Hello"}]}'
```

- 401: the key is invalid, disabled, or the `Bearer` prefix is missing.
- 404: the base URL or protocol does not match.
- Model not found: the key's group does not expose that model.

## Next

Pick your client from the left. Claude Code, Codex, Gemini CLI, and OpenCode can also use "Use key" on the Keys page to generate a configuration that matches the current key.
