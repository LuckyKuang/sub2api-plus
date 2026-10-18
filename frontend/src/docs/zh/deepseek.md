# DeepSeek · DSH Desktop

DeepSeek / DSH Desktop 使用网关的 OpenAI 兼容入口。控制台里的“出站身份”只影响网关到上游的请求，不影响你在客户端里填写的地址。

## 客户端设置

| 项 | 值 |
| --- | --- |
| Base URL | `{{openaiBaseUrl}}` |
| API Key | 在 [密钥页]({{keysUrl}}) 创建 |
| Model | `YOUR_MODEL_ID` |
| 协议 | OpenAI 兼容（以分组为准） |

## 验证

```bash os=unix
curl {{openaiBaseUrl}}/models \
  -H "Authorization: Bearer <YOUR_API_KEY>"
```

```bat os=cmd
curl {{openaiBaseUrl}}/models -H "Authorization: Bearer <YOUR_API_KEY>"
```

```powershell os=powershell
curl.exe {{openaiBaseUrl}}/models `
  -H "Authorization: Bearer <YOUR_API_KEY>"
```

返回模型列表后，在客户端新建会话测试。

## 注意

- 如果客户端要求填写完整端点，使用 `{{openaiBaseUrl}}/chat/completions`。
- 模型 ID 必须属于当前密钥的分组。
- 不要把 DeepSeek 官方 API 地址和本站地址混用。
