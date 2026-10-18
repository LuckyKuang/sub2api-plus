# Gemini CLI

Gemini CLI 通过环境变量读取网关地址和密钥。

官方文档：https://github.com/google-gemini/gemini-cli/blob/main/docs/reference/configuration.md

## 配置

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

> `GOOGLE_GEMINI_BASE_URL` 必须是 HTTPS，除非指向 `localhost`、`127.0.0.1` 或 `[::1]`。

## 验证

重新打开终端，运行 `gemini`，新建会话并发送一条消息。如果提示模型不存在，换用当前分组里的 Gemini 模型。

## Antigravity 分组

如果使用 Antigravity 分组，Base URL 改为 `{{baseUrl}}/antigravity`，详见 Antigravity 篇。
