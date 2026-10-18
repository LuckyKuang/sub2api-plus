# Antigravity

Google Antigravity 的官方文档提供 Google 账号登录、内置模型选择和 Antigravity CLI，但没有公开“自定义 API Base URL / API Key”的客户端入口。因此不要把 Antigravity 应用本身直接指向第三方网关；本站的 Antigravity 支持分为账号层和兼容客户端端点。

官方文档：https://antigravity.google/docs/cli/using

## 工作方式

1. 在后台添加 Antigravity 账号（OAuth 或 API Key），由网关调度上游请求。
2. 网关为兼容客户端提供两个专用端点：

| 客户端协议 | Base URL |
| --- | --- |
| Anthropic / Claude Code | `{{baseUrl}}/antigravity` |
| Gemini 原生 | `{{baseUrl}}/antigravity/v1beta` |

3. 在 Claude Code 或 Gemini CLI 中使用下面的配置，而不是修改 Antigravity 应用本身。

## Claude Code 风格

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

## Gemini 风格

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

## 说明

- Antigravity Claude 与普通 Claude 不要混在同一会话或同一分组上下文里。
- Antigravity 应用自身的模型选择、登录和 CLI 设置以 Google 官方文档为准；本文只描述网关提供的兼容端点。
- 模型 ID 以分组和密钥页为准。
