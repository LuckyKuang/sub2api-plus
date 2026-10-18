# Kimi Code

Kimi Code CLI reads `~/.kimi-code/config.toml`. It supports provider types such as `kimi`, `anthropic`, `openai`, and `openai_responses`; use `openai_responses` or `anthropic` for this deployment.

Official docs: https://moonshotai.github.io/kimi-code/en/configuration/providers

## Configuration file

<div class="docs-os-line" data-os="unix">macOS / Linux: <code>~/.kimi-code/config.toml</code></div>

<div class="docs-os-line" data-os="cmd">Windows CMD: <code>%USERPROFILE%\.kimi-code\config.toml</code></div>

<div class="docs-os-line" data-os="powershell">Windows PowerShell: <code>%USERPROFILE%\.kimi-code\config.toml</code></div>

## Responses group

```toml
default_model = "sub2api/YOUR_MODEL_ID"

[providers.sub2api]
type = "openai_responses"
base_url = "{{openaiBaseUrl}}"
api_key = "<YOUR_API_KEY>"

[models."sub2api/YOUR_MODEL_ID"]
provider = "sub2api"
model = "YOUR_MODEL_ID"
max_context_size = 200000
```

## Claude / Anthropic group

```toml
default_model = "sub2api/YOUR_MODEL_ID"

[providers.sub2api]
type = "anthropic"
base_url = "{{baseUrl}}"
api_key = "<YOUR_API_KEY>"

[models."sub2api/YOUR_MODEL_ID"]
provider = "sub2api"
model = "YOUR_MODEL_ID"
max_context_size = 200000
```

## Verify

Restart `kimi`, then use `/provider` to inspect providers or `/model` to choose a model. After editing `config.toml`, use `/reload` to reload it.

## Notes

- Kimi Code does not automatically read `KIMI_API_KEY` from the shell. Unless the provider explicitly uses `api_key_env`, put the key in `api_key`.
- Kimi Code OAuth accounts created through `/login` do not appear in `/provider`; manage them with `/login` and `/logout`.
- Never commit a real API key.
