# Grok CLI

Grok Build CLI reads `~/.grok/config.toml`. The official custom-model format uses `[model.<id>]`; `api_backend = "responses"` selects the Responses protocol used by this deployment.

Official docs: https://docs.x.ai/build/settings/reference

## Configuration file

<div class="docs-os-line" data-os="unix">macOS / Linux: <code>~/.grok/config.toml</code></div>

<div class="docs-os-line" data-os="cmd">Windows CMD: <code>%USERPROFILE%\.grok\config.toml</code></div>

<div class="docs-os-line" data-os="powershell">Windows PowerShell: <code>%USERPROFILE%\.grok\config.toml</code></div>

## Set the API key

```bash os=unix
export XAI_API_KEY="<YOUR_API_KEY>"
```

```bat os=cmd
set XAI_API_KEY=<YOUR_API_KEY>
```

```powershell os=powershell
$env:XAI_API_KEY="<YOUR_API_KEY>"
```

## Add a custom model

```toml
[model.sub2api]
model = "YOUR_MODEL_ID"
base_url = "{{openaiBaseUrl}}"
name = "Sub2API Plus"
env_key = "XAI_API_KEY"
api_backend = "responses"
context_window = 200000

[models]
default = "sub2api"
```

## Verify

```bash
grok inspect
grok -p "Hello" -m sub2api
```

`grok inspect` shows the config sources, models, and MCP servers discovered in the current directory; you can also switch with `/model` in the TUI.

## Notes

- `base_url` must include `/v1`; `{{openaiBaseUrl}}` already does.
- For an Anthropic Messages group, change `api_backend` to `messages` and `base_url` to `{{baseUrl}}`.
- Never commit `XAI_API_KEY` to a repository or version control.
