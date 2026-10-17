# StepFun · Step-Code

StepFun / Step-Code 支持 Responses 和 Messages 客户端。客户端连接使用控制台 API Key。

## 配置

| 项 | 值 |
| --- | --- |
| OpenAI / Responses Base URL | `{{openaiBaseUrl}}` |
| Anthropic / Messages Base URL | `{{baseUrl}}` |
| API Key | 在 [密钥页]({{keysUrl}}) 创建 |
| Model | `YOUR_MODEL_ID` |

## 选择协议

- Codex、OpenCode 等 Responses 客户端：使用 `{{openaiBaseUrl}}`。
- Claude Code 等 Messages 客户端：使用 `{{baseUrl}}`。
- 不要同时把两套协议混在同一个客户端配置里。

## 注意

- Step-Code 是版本号免填的客户端家族；后台不需要填写版本。
- 模型 ID 以分组和密钥页为准。
