# Antigravity

Antigravity uses dedicated endpoints. Choose by client protocol:

| Client protocol | Base URL |
| --- | --- |
| Anthropic / Claude Code | `{{baseUrl}}/antigravity` |
| Gemini native | `{{baseUrl}}/antigravity/v1beta` |

## Claude Code style

```bash os=unix
export ANTHROPIC_BASE_URL="{{baseUrl}}/antigravity"
export ANTHROPIC_AUTH_TOKEN="<YOUR_API_KEY>"
```

```bat os=cmd
set ANTHROPIC_BASE_URL={{baseUrl}}/antigravity
set ANTHROPIC_AUTH_TOKEN=<YOUR_API_KEY>
```

```powershell os=powershell
$env:ANTHROPIC_BASE_URL="{{baseUrl}}/antigravity"
$env:ANTHROPIC_AUTH_TOKEN="<YOUR_API_KEY>"
```

## Gemini style

In Gemini CLI or another client that supports Gemini native protocol, set the base URL to `{{baseUrl}}/antigravity/v1beta` and use the same API key.

## OpenCode

Select an Antigravity group on the Keys page, click "Use key", and choose OpenCode. It generates both Claude and Gemini `opencode.json` variants.

## Notes

- Do not mix Antigravity Claude and regular Claude in the same conversation or group context.
- Model IDs come from the group and the Keys page.
