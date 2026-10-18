# GLM · ZCode

ZCode is Zhipu's graphical agent client. The official configuration entry is **Settings → Model Providers**, not shell environment variables. To use this deployment, point its OpenAI and Anthropic base URLs at this gateway.

Official docs: https://docs.z.ai/devpack/tool/zcode

## Configuration

1. Open ZCode and choose **Settings** in the lower-left corner.
2. Select **Model Providers** in the left sidebar.
3. Enter your API key in the **Z.ai Open Platform** section.
4. Set the base URLs manually:

| Protocol | Base URL |
| --- | --- |
| OpenAI-compatible | `{{openaiBaseUrl}}` |
| Anthropic / Claude-compatible | `{{baseUrl}}` |

5. Save, then choose `YOUR_MODEL_ID` in the model switcher.

## Notes

- ZCode is a graphical client; the steps are the same on macOS, Windows, and Linux, with no CMD or PowerShell difference.
- Do not confuse this with the admin panel's "ZCode account link": account linking gives the gateway an upstream credential, while client connections use a panel API key.
- The official docs use `https://api.z.ai/api/coding/paas/v4` and `https://api.z.ai/api/anthropic`; replace them with the base URLs above for this deployment.
- If ZCode appends `/v1` to the base URL automatically, use `{{baseUrl}}` as the OpenAI base URL to avoid `/v1/v1`.
- Model IDs come from the group and the Keys page.
