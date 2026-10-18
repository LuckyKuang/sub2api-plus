# Gemini CLI

Gemini CLI reads the gateway address and key from environment variables.

## Configuration

```bash os=unix
export GOOGLE_GEMINI_BASE_URL="{{baseUrl}}"
export GEMINI_API_KEY="<YOUR_API_KEY>"
export GEMINI_MODEL="YOUR_MODEL_ID"
```

```bat os=cmd
set GOOGLE_GEMINI_BASE_URL={{baseUrl}}
set GEMINI_API_KEY=<YOUR_API_KEY>
set GEMINI_MODEL=YOUR_MODEL_ID
```

```powershell os=powershell
$env:GOOGLE_GEMINI_BASE_URL="{{baseUrl}}"
$env:GEMINI_API_KEY="<YOUR_API_KEY>"
$env:GEMINI_MODEL="YOUR_MODEL_ID"
```

## Verify

Open a new terminal, run `gemini`, start a session, and send a message. If the model is not found, use a Gemini model from the current group.

## Antigravity groups

For an Antigravity group, use `{{baseUrl}}/antigravity` as the base URL. See the Antigravity article.
