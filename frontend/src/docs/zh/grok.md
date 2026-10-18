# Grok CLI

Grok Build CLI 读取 `~/.grok/config.toml`。官方通过 `[model.<id>]` 添加自定义模型，`api_backend = "responses"` 对应本站的 Responses 协议。

官方文档：https://docs.x.ai/build/settings/reference

## 配置文件

<div class="docs-os-line" data-os="unix">macOS / Linux：<code>~/.grok/config.toml</code></div>

<div class="docs-os-line" data-os="cmd">Windows CMD：<code>%USERPROFILE%\.grok\config.toml</code></div>

<div class="docs-os-line" data-os="powershell">Windows PowerShell：<code>%USERPROFILE%\.grok\config.toml</code></div>

## 设置 API Key

```bash os=unix
export XAI_API_KEY="<YOUR_API_KEY>"
```

```bat os=cmd
set XAI_API_KEY=<YOUR_API_KEY>
```

```powershell os=powershell
$env:XAI_API_KEY="<YOUR_API_KEY>"
```

## 添加自定义模型

```toml
[model.sub2api]
model = "YOUR_MODEL_ID"
base_url = "{{openaiBaseUrl}}"
name = "Sub2API Plus"
env_key = "XAI_API_KEY"
api_backend = "responses"
context_window = 200000

[models]
default = "sub2api"
```

## 验证

```bash
grok inspect
grok -p "Hello" -m sub2api
```

`grok inspect` 会显示当前目录发现的配置来源、模型和 MCP；在 TUI 中也可以用 `/model` 切换。

## 说明

- `base_url` 必须包含 `/v1`；上面的 `{{openaiBaseUrl}}` 已经处理。
- 如果分组使用 Anthropic Messages，把 `api_backend` 改为 `messages`，`base_url` 改为 `{{baseUrl}}`。
- 不要把 `XAI_API_KEY` 写入仓库或提交到版本控制。
