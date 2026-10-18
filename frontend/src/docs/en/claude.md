# Claude Code

Claude Code uses the Anthropic Messages protocol. Set the base URL to `{{baseUrl}}` and authenticate with `ANTHROPIC_AUTH_TOKEN`.

## Configuration file

<div class="docs-os-line" data-os="unix">macOS / Linux: <code>~/.claude/settings.json</code></div>

<div class="docs-os-line" data-os="cmd">Windows CMD: <code>%USERPROFILE%\.claude\settings.json</code></div>

<div class="docs-os-line" data-os="powershell">Windows PowerShell: <code>%USERPROFILE%\.claude\settings.json</code></div>

```json
{
  "$schema": "https://json.schemastore.org/claude-code-settings.json",
  "env": {
    "ANTHROPIC_BASE_URL": "{{baseUrl}}",
    "ANTHROPIC_AUTH_TOKEN": "<YOUR_API_KEY>",
    "CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC": "1"
  }
}
```

## Or use environment variables

```bash os=unix
export ANTHROPIC_BASE_URL="{{baseUrl}}"
export ANTHROPIC_AUTH_TOKEN="<YOUR_API_KEY>"
export CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC=1
```

```bat os=cmd
set ANTHROPIC_BASE_URL={{baseUrl}}
set ANTHROPIC_AUTH_TOKEN=<YOUR_API_KEY>
set CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC=1
```

```powershell os=powershell
$env:ANTHROPIC_BASE_URL="{{baseUrl}}"
$env:ANTHROPIC_AUTH_TOKEN="<YOUR_API_KEY>"
$env:CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC="1"
```

## Model

`ANTHROPIC_MODEL` is optional. The model must exist in the key's group; "Use key" on the Keys page shows the recommended value.

## Notes

- Use `ANTHROPIC_AUTH_TOKEN`, not `ANTHROPIC_API_KEY`.
- If the base URL already ends with `/v1`, do not add another one.
- Quit and restart Claude Code after changing the configuration.
