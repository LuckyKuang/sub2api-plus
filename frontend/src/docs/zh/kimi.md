# Kimi Code

Kimi Code CLI 的配置写在 `~/.kimi-code/config.toml`。它支持 `kimi`、`anthropic`、`openai`、`openai_responses` 等供应商类型；接入本站时推荐使用 `openai_responses` 或 `anthropic`。

官方文档：https://moonshotai.github.io/kimi-code/en/configuration/providers

## 配置文件

<div class="docs-os-line" data-os="unix">macOS / Linux：<code>~/.kimi-code/config.toml</code></div>

<div class="docs-os-line" data-os="cmd">Windows CMD：<code>%USERPROFILE%\.kimi-code\config.toml</code></div>

<div class="docs-os-line" data-os="powershell">Windows PowerShell：<code>%USERPROFILE%\.kimi-code\config.toml</code></div>

## Responses 分组

```toml
default_model = "sub2api/YOUR_MODEL_ID"

[providers.sub2api]
type = "openai_responses"
base_url = "{{openaiBaseUrl}}"
api_key = "<YOUR_API_KEY>"

[models."sub2api/YOUR_MODEL_ID"]
provider = "sub2api"
model = "YOUR_MODEL_ID"
max_context_size = 200000
```

## Claude / Anthropic 分组

```toml
default_model = "sub2api/YOUR_MODEL_ID"

[providers.sub2api]
type = "anthropic"
base_url = "{{baseUrl}}"
api_key = "<YOUR_API_KEY>"

[models."sub2api/YOUR_MODEL_ID"]
provider = "sub2api"
model = "YOUR_MODEL_ID"
max_context_size = 200000
```

## 验证

重新启动 `kimi`，用 `/provider` 查看供应商，或用 `/model` 选择模型。修改 `config.toml` 后可用 `/reload` 重新加载。

## 说明

- Kimi Code 不会自动读取 shell 里的 `KIMI_API_KEY`；除非在供应商里显式写 `api_key_env`，否则必须写 `api_key`。
- 通过 `/login` 登录的 Kimi Code 托管账号不会出现在 `/provider` 中，用 `/login` 和 `/logout` 管理。
- 不要把真实 API Key 写进仓库。
