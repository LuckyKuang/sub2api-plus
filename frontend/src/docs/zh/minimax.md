# MiniMax Code

MiniMax Code 有两种后台账号模式：OAuth 和 API Key。客户端连接都使用控制台创建的 API Key；后台账号模式只影响上游调度。

## 通用配置

| 项 | 值 |
| --- | --- |
| Base URL | `{{baseUrl}}` |
| API Key | 在 [密钥页]({{keysUrl}}) 创建 |
| Model | `YOUR_MODEL_ID` |
| 协议 | Anthropic / Claude 兼容（以分组为准） |

## OAuth 模式

在后台“账号管理”里完成 MiniMax OAuth 授权。客户端仍然填写上面的 Base URL 和 API Key。

## API Key 模式

在后台添加 MiniMax API Key 账号。客户端配置与 OAuth 模式相同；区别只在后台账号类型。

## Anthropic 兼容客户端

如果客户端使用 Anthropic 兼容环境变量：

```bash os=unix
export ANTHROPIC_BASE_URL="{{baseUrl}}"
export ANTHROPIC_AUTH_TOKEN="<YOUR_API_KEY>"
```

```bat os=cmd
set ANTHROPIC_BASE_URL={{baseUrl}}
set ANTHROPIC_AUTH_TOKEN=<YOUR_API_KEY>
```

```powershell os=powershell
$env:ANTHROPIC_BASE_URL="{{baseUrl}}"
$env:ANTHROPIC_AUTH_TOKEN="<YOUR_API_KEY>"
```

## 验证

先请求一次模型列表或发送一条短消息。如果返回 401，检查客户端填的是控制台 Key，而不是 MiniMax 官方 Key。

## 注意

- 不要把 OAuth 账号的 token 直接填到客户端。
- 模型 ID 以分组和密钥页为准。
