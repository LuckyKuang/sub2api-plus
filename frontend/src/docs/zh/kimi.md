# Kimi Code

Kimi Code 使用 Anthropic / Claude 兼容入口。后台的“Kimi Code”出站身份用于上游请求，不影响客户端填写的地址。

## 配置

| 项 | 值 |
| --- | --- |
| Base URL | `{{baseUrl}}` |
| API Key | 在 [密钥页]({{keysUrl}}) 创建 |
| Model | `YOUR_MODEL_ID` |
| 协议 | Anthropic / Claude 兼容（以分组为准） |

## 如果客户端支持环境变量

```bash
export ANTHROPIC_BASE_URL="{{baseUrl}}"
export ANTHROPIC_AUTH_TOKEN="<YOUR_API_KEY>"
```

## 如果客户端只支持自定义 provider

选择 Anthropic / Claude 兼容 provider，填入 Base URL、API Key 和模型；不要把 `X-Msh-*` 设备头手动写进客户端配置，网关会按后台身份设置生成。

## 注意

- Kimi Code 的模型 ID 和会员权限相关，以分组和密钥页为准。
- 设备声明、语言和时区在后台“出站身份 > Kimi Code”里配置。
