# Antigravity

Google Antigravity's official documentation covers Google-account sign-in, built-in model selection, and the Antigravity CLI. It does not document a custom API base URL or API key entry for the Antigravity app itself. Do not point the Antigravity app at a third-party gateway; this deployment's Antigravity support has two separate parts: upstream accounts and compatible client endpoints.

Official docs: https://antigravity.google/docs/cli/using

## How it works

1. Add an Antigravity account (OAuth or API key) in the admin panel for upstream scheduling.
2. The gateway exposes two dedicated endpoints for compatible clients:

| Client protocol | Base URL |
| --- | --- |
| Anthropic / Claude Code | `{{baseUrl}}/antigravity` |
| Gemini native | `{{baseUrl}}/antigravity/v1beta` |

3. Configure Claude Code or Gemini CLI below; do not modify the Antigravity app itself.

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

```bash os=unix
export GOOGLE_GEMINI_BASE_URL="{{baseUrl}}/antigravity"
export GEMINI_API_KEY="<YOUR_API_KEY>"
```

```bat os=cmd
set GOOGLE_GEMINI_BASE_URL={{baseUrl}}/antigravity
set GEMINI_API_KEY=<YOUR_API_KEY>
```

```powershell os=powershell
$env:GOOGLE_GEMINI_BASE_URL="{{baseUrl}}/antigravity"
$env:GEMINI_API_KEY="<YOUR_API_KEY>"
```

## Notes

- Do not mix Antigravity Claude and regular Claude in the same conversation or group context.
- Model selection, sign-in, and CLI settings inside the Antigravity app follow Google's official docs; this page only covers the gateway's compatibility endpoints.
- Model IDs come from the group and the Keys page.
