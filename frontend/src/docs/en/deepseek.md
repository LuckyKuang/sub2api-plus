# DeepSeek · DSH Desktop

DSH (DeepSeek Harness) is DeepSeek's official agent harness. Its Desktop and Web UIs share model configuration. The official path is **Settings → Models → Add model provider → Custom model API**, not an `OPENAI_BASE_URL` environment variable.

Official docs: https://deepseek-harness.github.io/deepseek-harness/en/guide/providers

## Add through the UI

1. Open DSH and go to **Settings → Models**.
2. Choose **Add model provider** and switch to **Custom model API**.
3. Fill in:

| Item | Value |
| --- | --- |
| Provider ID | `sub2api` (lowercase; permanent after saving) |
| Display name | `Sub2API Plus` |
| Base URL | `{{openaiBaseUrl}}` |
| API protocol | `OpenAI Responses` |
| API Key | Create one on the [Keys page]({{keysUrl}}) |
| Models | `YOUR_MODEL_ID` |

4. Save, then select the model in the model picker.

## Protocol selection

- Responses group: `Base URL = {{openaiBaseUrl}}`, protocol `OpenAI Responses`.
- Chat Completions group: `Base URL = {{openaiBaseUrl}}`, protocol `OpenAI Chat Completions`.
- Claude / Anthropic group: `Base URL = {{baseUrl}}`, protocol `Anthropic Messages`.
- A provider speaks one protocol. To use both, create two IDs, such as `sub2api-responses` and `sub2api-anthropic`.

## Configuration file

<div class="docs-os-line" data-os="unix">macOS / Linux: <code>~/.dsh/profiles/web/cordis.patch.yml</code></div>

<div class="docs-os-line" data-os="cmd">Windows CMD: <code>%USERPROFILE%\.dsh\profiles\web\cordis.patch.yml</code></div>

<div class="docs-os-line" data-os="powershell">Windows PowerShell: <code>%USERPROFILE%\.dsh\profiles\web\cordis.patch.yml</code></div>

Advanced configuration can edit this file directly; the adapter rereads it on the next request without a restart:

```yaml
- id: llm-pi-ai
  config:
    providers:
      sub2api:
        apiKeyEnv: SUB2API_API_KEY
        api: openai-responses
        baseURL: "{{openaiBaseUrl}}"
        models:
          - id: YOUR_MODEL_ID
```

With `apiKeyEnv`, set `SUB2API_API_KEY` in the environment that launches DSH; alternatively keep using the credential saved through the UI.

## Notes

- DSH stores data under `~/.dsh` by default, or `%USERPROFILE%\.dsh` on Windows.
- The official docs state that a Provider ID is permanent once saved; add a new provider and remove the old one to rename it.
- Model IDs come from the group and the Keys page.
