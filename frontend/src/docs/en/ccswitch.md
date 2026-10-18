# CC Switch

CC Switch is a desktop provider switcher for Claude Code, Codex, Gemini CLI, Grok Build, and other clients. It only replaces the endpoint, key, and model; your existing MCP servers, skills, hooks, and comments stay intact.

## Recommended: import from the Keys page

1. Open the [Keys page]({{keysUrl}}).
2. Find the target key and click "Import to CCS".
3. In CC Switch, confirm the provider name, endpoint, API key, and model.
4. Choose direct mode, enable the provider, and restart the client.

The import uses a `ccswitch://v1/import` deep link with these endpoints:

- Claude Code: `{{baseUrl}}`
- Codex: `{{baseUrl}}` (CC Switch appends the OpenAI-compatible paths)
- Gemini CLI: `{{baseUrl}}`
- Grok Build: `{{openaiBaseUrl}}`
- Antigravity: `{{baseUrl}}/antigravity`

## Add manually

In CC Switch, select the tool → add provider → custom configuration, then fill in:

| Item | Value |
| --- | --- |
| Name | Any, for example `Sub2API Plus` |
| Endpoint / Base URL | Choose per client above |
| API Key | Create one on the [Keys page]({{keysUrl}}) |
| Model | `YOUR_MODEL_ID` |
| Config format | JSON (Claude / Codex / Gemini / Grok) |

If CC Switch asks for a model, use a model ID from the key's group; do not use the provider's official model name.

## Config file locations

<div class="docs-os-line" data-os="unix">macOS / Linux: <code>~/.cc-switch/config.json</code></div>

<div class="docs-os-line" data-os="cmd">Windows CMD: <code>%USERPROFILE%\.cc-switch\config.json</code></div>

<div class="docs-os-line" data-os="powershell">Windows PowerShell: <code>%USERPROFILE%\.cc-switch\config.json</code></div>

CC Switch also writes the provider into each client's own config file. Do not edit the same client config manually at the same time, or a later switch may overwrite your changes. CC Switch backs up the original file before its first rewrite.

## Notes

- This article describes **direct** mode: requests go straight to this deployment.
- If you enable CC Switch local routing, point the client at CC Switch's local port instead of this deployment; follow the CC Switch routing panel in that case.
- The administrator can hide the "Import to CCS" button under **Site Settings → Hide CCS Import Button**. If it is hidden, use the manual steps above.
- CC Switch is a third-party tool; its versions and menus may change. Follow the official CC Switch documentation when they differ.

Official project: https://github.com/farion1231/cc-switch
