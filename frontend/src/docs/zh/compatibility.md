# 兼容客户端

以下客户端没有独立的出站身份，默认映射到 Codex 或 OpenAI 兼容入口。它们仍然可以正常使用，只是后台“出站身份”里显示为 Codex。

## OpenCode

推荐在密钥页点击“使用密钥”选择 OpenCode，直接生成 `opencode.json`。手动配置的最小示例：

```json
{
  "$schema": "https://opencode.ai/config.json",
  "provider": {
    "sub2api": {
      "npm": "@ai-sdk/openai-compatible",
      "name": "Sub2API Plus",
      "options": {
        "baseURL": "{{openaiBaseUrl}}",
        "apiKey": "<YOUR_API_KEY>"
      },
      "models": {
        "YOUR_MODEL_ID": { "name": "YOUR_MODEL_ID" }
      }
    }
  }
}
```

## Cline / Command Code

在 provider 设置里选择 OpenAI Compatible：

- Base URL：`{{openaiBaseUrl}}`
- API Key：控制台密钥
- Model：`YOUR_MODEL_ID`

## TypeSafe / System One

TypeSafe / System One 使用原生端点，不走 OpenAI 兼容路径。请在密钥页选择 TypeSafe 分组，点击“使用密钥”复制对应的 curl 配置。

## 注意

- 兼容客户端只改变客户端的接入方式，不改变后台出站身份。
- 如果客户端要求 `/v1`，不要重复添加。
