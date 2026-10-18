# MiniMax Code

The MiniMax Code CLI is `mcode`. The official way to add a custom provider is `mcode provider add`, which lets you use this deployment as an OpenAI-compatible or Anthropic-compatible endpoint.

Official docs: https://agent.minimax.io/docs/cli/configuration

## Set the API key

```bash os=unix
export MCODE_PROVIDER_API_KEY="<YOUR_API_KEY>"
```

```bat os=cmd
set MCODE_PROVIDER_API_KEY=<YOUR_API_KEY>
```

```powershell os=powershell
$env:MCODE_PROVIDER_API_KEY="<YOUR_API_KEY>"
```

## Add a provider

```bash
mcode provider add \
  --name sub2api \
  --base-url "{{openaiBaseUrl}}" \
  --api-format openai-responses \
  --model "YOUR_MODEL_ID" \
  --api-key-env MCODE_PROVIDER_API_KEY \
  --use
```

- `--api-format` accepts `openai-completions`, `openai-responses`, or `anthropic-messages`.
- Use `openai-responses` for a Responses group, `openai-completions` for Chat Completions, or `anthropic-messages` for a Claude group (with `--base-url "{{baseUrl}}"`).
- `--use` tests the first model before saving; a failed test stores nothing.

## Verify

```bash
mcode provider list
mcode provider test sub2api --model "YOUR_MODEL_ID"
```

Inside `mcode`, use `/provider` to choose the provider or `/model` to switch models.

## Notes

- OAuth / Token Plan and custom providers are separate paths; use the API-key provider above to connect to this deployment.
- MiniMax Code stores user data under `~/.minimax` by default, or `%USERPROFILE%\.minimax` on Windows.
- Never commit a real API key.
