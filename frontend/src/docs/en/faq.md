# FAQ

## 401 Unauthorized

- The key is incomplete, deleted, or disabled.
- The request does not use `Authorization: Bearer <KEY>`.
- An Anthropic client put the key in an OAuth token field.
- The client is using an upstream account key instead of a panel key.

## 404 Not Found

- The base URL has an extra or missing `/v1`.
- An Anthropic client is calling the OpenAI `/chat/completions` path.
- A Gemini client should use `/v1beta`.
- A reverse proxy rewrites the path.

## Model not found

- The model ID does not belong to the key's group.
- The model is not visible in Model Plaza.
- The example `YOUR_MODEL_ID` was submitted unchanged.

## Configuration does not take effect

- Quit the client completely and restart it; closing the window is not enough.
- Check the shell profile for stale environment variables.
- Codex: check `wire_api = "responses"`.
- Claude Code: check that `ANTHROPIC_AUTH_TOKEN` is used.
- OpenCode: check that `baseURL` points to `{{openaiBaseUrl}}`.

## WebSocket connection fails

- WebSocket is not enabled for the group or account.
- Switch back to HTTP/SSE temporarily and confirm that basic requests work.
- Codex: check whether `supports_websockets = false` is still present.

## Proxy / direct connection

See [Network and proxy](/docs/network).

## Still stuck

When contacting the administrator, include the client name and version, base URL, request time, error code, and error message. Never send the full API key.
