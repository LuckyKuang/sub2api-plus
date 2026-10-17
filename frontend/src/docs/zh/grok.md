# Grok CLI

Grok CLI 读取 `~/.grok/config.toml`。文本模型使用 Responses 协议（`api_backend = "responses"`）。

## 环境变量

```bash
export GROK_MODELS_BASE_URL="{{baseUrl}}"
export XAI_API_KEY="<YOUR_API_KEY>"
```

## 配置

```toml
[endpoints]
models_base_url = "{{baseUrl}}"
models_list_url = "{{baseUrl}}/models"
xai_api_base_url = "{{baseUrl}}"
cli_chat_proxy_base_url = "{{baseUrl}}"

[auth]
preferred_method = "api_key"

[model."YOUR_MODEL_ID"]
model = "YOUR_MODEL_ID"
name = "YOUR_MODEL_ID"
env_key = "XAI_API_KEY"
api_backend = "responses"
context_window = 500000

[models]
default = "YOUR_MODEL_ID"
web_search = "YOUR_MODEL_ID"
image_description = "YOUR_MODEL_ID"
```

## 验证

保存后运行 `grok inspect`，确认 endpoint、认证方式和模型都被识别；再新建会话发送消息。

## 注意

- 不要把 `api_backend` 留空，否则可能走 Chat Completions。
- 图片/视频模型使用 Imagine 模型 ID 和媒体端点，不要混用文本配置。
