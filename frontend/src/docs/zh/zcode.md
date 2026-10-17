# GLM · ZCode

GLM / ZCode 使用 Anthropic / Claude 兼容入口。不要把它和后台的“ZCode 账号关联”混淆：账号关联是让网关获得上游凭证，客户端连接仍然使用控制台 API Key。

## 配置

| 项 | 值 |
| --- | --- |
| Base URL | `{{baseUrl}}` |
| API Key | 在 [密钥页]({{keysUrl}}) 创建 |
| Model | `YOUR_MODEL_ID` |
| 协议 | Anthropic / Claude 兼容（以分组为准） |

## Claude Code 风格

```bash
export ANTHROPIC_BASE_URL="{{baseUrl}}"
export ANTHROPIC_AUTH_TOKEN="<YOUR_API_KEY>"
```

## 注意

- 后台“出站身份 > GLM · ZCode”里的语言、时区、平台声明只影响网关到上游的请求。
- 模型 ID 以分组和密钥页为准。
- 如果使用 Coding Plan，确认账号和分组已经绑定。
