# Antigravity

Antigravity 使用独立端点。根据客户端协议选择：

| 客户端协议 | Base URL |
| --- | --- |
| Anthropic / Claude Code | `{{baseUrl}}/antigravity` |
| Gemini 原生 | `{{baseUrl}}/antigravity/v1beta` |

## Claude Code 风格

```bash
export ANTHROPIC_BASE_URL="{{baseUrl}}/antigravity"
export ANTHROPIC_AUTH_TOKEN="<YOUR_API_KEY>"
```

## Gemini 风格

在 Gemini CLI 或支持 Gemini 原生协议的客户端中，把 Base URL 设为 `{{baseUrl}}/antigravity/v1beta`，密钥填同一个 API Key。

## OpenCode

在密钥页选择 Antigravity 分组后点击“使用密钥”，选择 OpenCode，会生成 Claude 和 Gemini 两份 `opencode.json`。

## 注意

- Antigravity Claude 与普通 Claude 不要混在同一会话或同一分组上下文里。
- 模型 ID 以分组和密钥页为准。
