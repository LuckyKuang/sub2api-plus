# StepFun · Step-Code

Step Code adds custom providers through `~/.stepcode/models.json`. It supports `openai-completions`, `openai-responses`, `anthropic-messages`, and other protocols; use Responses or Anthropic Messages for this deployment.

Official docs: https://platform.stepfun.com/docs/zh/step-code/configuration/models

## Configuration file

<div class="docs-os-line" data-os="unix">macOS / Linux: <code>~/.stepcode/models.json</code></div>

<div class="docs-os-line" data-os="cmd">Windows CMD: <code>%USERPROFILE%\.stepcode\models.json</code></div>

<div class="docs-os-line" data-os="powershell">Windows PowerShell: <code>%USERPROFILE%\.stepcode\models.json</code></div>

## Responses group

```json
{
  "providers": {
    "sub2api": {
      "baseUrl": "{{openaiBaseUrl}}",
      "api": "openai-responses",
      "apiKey": "<YOUR_API_KEY>",
      "models": [
        { "id": "YOUR_MODEL_ID" }
      ]
    }
  }
}
```

## Claude / Anthropic group

```json
{
  "providers": {
    "sub2api": {
      "baseUrl": "{{baseUrl}}",
      "api": "anthropic-messages",
      "apiKey": "<YOUR_API_KEY>",
      "models": [
        { "id": "YOUR_MODEL_ID" }
      ]
    }
  }
}
```

## Verify

Save the file, reopen `/model`, and choose `sub2api/YOUR_MODEL_ID`. You can also run:

```bash
step --provider sub2api --model YOUR_MODEL_ID
```

## Notes

- For OpenAI-compatible endpoints, `baseUrl` must include `/v1`; `{{openaiBaseUrl}}` already does.
- `apiKey` is stored in the local JSON file; never commit a real key.
- Do not point `STEP_API_KEY` / `STEP_BASE_URL` at this deployment; those belong to StepFun's official Step platform.
