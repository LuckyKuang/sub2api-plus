# Codex

Codex CLI and Codex Desktop use the OpenAI Responses protocol. The recommended path is "Use key" on the Keys page: choose Codex CLI and copy the generated configuration. The template below is the generic form.

## Configuration file

macOS / Linux: `~/.codex/config.toml`

Windows: `%USERPROFILE%\.codex\config.toml`

```toml
model_provider = "sub2api"
model = "YOUR_MODEL_ID"

[model_providers.sub2api]
name = "Sub2API Plus"
base_url = "{{baseUrl}}"
env_key = "SUB2API_API_KEY"
wire_api = "responses"
requires_openai_auth = false
supports_websockets = false
```

## Set the environment variable

```bash
export SUB2API_API_KEY="<YOUR_API_KEY>"
```

PowerShell:

```powershell
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
