# DeepSeek · DSH Desktop

DeepSeek / DSH Desktop uses the gateway's OpenAI-compatible entry point. The "outbound identity" settings only affect gateway-to-upstream requests; they do not change the address you enter in the client.

## Client settings

| Item | Value |
| --- | --- |
| Base URL | `{{openaiBaseUrl}}` |
| API Key | Create one on the [Keys page]({{keysUrl}}) |
| Model | `YOUR_MODEL_ID` |
| Protocol | OpenAI-compatible (per group) |

## Verify

```bash
curl {{openaiBaseUrl}}/models \
  -H "Authorization: Bearer <YOUR_API_KEY>"
```

After the model list returns, create a session in the client and send a message.

## Notes

- If the client asks for a full endpoint, use `{{openaiBaseUrl}}/chat/completions`.
- The model ID must belong to the key's group.
- Do not mix the official DeepSeek API address with this deployment's address.
