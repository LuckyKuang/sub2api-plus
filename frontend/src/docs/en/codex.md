# Codex

Codex CLI and Codex Desktop use the OpenAI Responses protocol. The recommended path is "Use key" on the Keys page: choose Codex CLI and copy the generated configuration. The template below is the generic form.

Official docs: https://developers.openai.com/codex/config-reference

## Configuration file

<div class="docs-os-line" data-os="unix">macOS / Linux: <code>~/.codex/config.toml</code></div>

<div class="docs-os-line" data-os="cmd">Windows CMD: <code>%USERPROFILE%\.codex\config.toml</code></div>

<div class="docs-os-line" data-os="powershell">Windows PowerShell: <code>%USERPROFILE%\.codex\config.toml</code></div>

```toml
model_provider = "OpenAI"
model = "YOUR_MODEL_ID"

[model_providers.OpenAI]
name = "OpenAI"
base_url = "{{baseUrl}}"
env_key = "SUB2API_API_KEY"
wire_api = "responses"
requires_openai_auth = false
supports_websockets = false
```

> The `name` under `[model_providers.OpenAI]` must be exactly `OpenAI` for Codex to enable remote compaction by default. This guide also uses `OpenAI` as the provider key for consistency; if you use a custom key, keep `name = "OpenAI"` or set `capabilities.remote_compaction = "v2"` explicitly.

## Set the environment variable

```bash os=unix
export SUB2API_API_KEY="<YOUR_API_KEY>"
```

```bat os=cmd
set SUB2API_API_KEY=<YOUR_API_KEY>
```

```powershell os=powershell
$env:SUB2API_API_KEY="<YOUR_API_KEY>"
```

Legacy compatibility mode also needs `auth.json`:

```json
{ "OPENAI_API_KEY": "<YOUR_API_KEY>" }
```

API Key mode does not need `auth.json`.

## Verify

Quit Codex completely, restart it, and create a new task. Existing sessions do not reload the configuration.

## WebSocket

If the group and account support WebSocket, choose "Codex CLI (WebSocket)" on the Keys page. Do not keep `supports_websockets = false` when using WS.
