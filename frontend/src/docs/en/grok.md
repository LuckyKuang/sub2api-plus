# Grok CLI

Grok CLI reads `~/.grok/config.toml`. Text models use the Responses protocol (`api_backend = "responses"`).

## Environment variables

```bash
export GROK_MODELS_BASE_URL="{{baseUrl}}"
export XAI_API_KEY="<YOUR_API_KEY>"
```

## Configuration

```toml
[endpoints]
models_base_url = "{{baseUrl}}"
models_list_url = "{{baseUrl}}/models"
xai_api_base_url = "{{baseUrl}}"
cli_chat_proxy_base_url = "{{baseUrl}}"

[auth]
preferred_method = "api_key"

[model."YOUR_MODEL_ID"]
model = "YOUR_MODEL_ID"
name = "YOUR_MODEL_ID"
env_key = "XAI_API_KEY"
api_backend = "responses"
context_window = 500000

[models]
default = "YOUR_MODEL_ID"
web_search = "YOUR_MODEL_ID"
image_description = "YOUR_MODEL_ID"
```

## Verify

Run `grok inspect` and confirm the endpoint, authentication method, and model are recognized. Then start a new session and send a message.

## Notes

- Do not leave `api_backend` empty; it may fall back to Chat Completions.
- Image and video models use Imagine IDs and media endpoints. Do not mix them into text configuration.
