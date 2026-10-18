# Codex

Codex CLI 和 Codex Desktop 使用 OpenAI Responses 协议。推荐在密钥页点击“使用密钥”选择 Codex CLI，直接复制生成好的配置；下面是通用模板。

## 配置文件

<div class="docs-os-line" data-os="unix">macOS / Linux：<code>~/.codex/config.toml</code></div>

<div class="docs-os-line" data-os="cmd">Windows CMD：<code>%USERPROFILE%\.codex\config.toml</code></div>

<div class="docs-os-line" data-os="powershell">Windows PowerShell：<code>%USERPROFILE%\.codex\config.toml</code></div>

```toml
model_provider = "OpenAI"
model = "YOUR_MODEL_ID"

[model_providers.OpenAI]
name = "OpenAI"
base_url = "{{baseUrl}}"
env_key = "SUB2API_API_KEY"
wire_api = "responses"
requires_openai_auth = false
supports_websockets = false
```

> `model_provider` 和 `[model_providers.*]` 的 key 必须使用 `OpenAI`，Codex 才会启用远程压缩（remote compaction）。不要改成 `sub2api` 或其他自定义名称。

## 设置环境变量

```bash os=unix
export SUB2API_API_KEY="<YOUR_API_KEY>"
```

```bat os=cmd
set SUB2API_API_KEY=<YOUR_API_KEY>
```

```powershell os=powershell
$env:SUB2API_API_KEY="<YOUR_API_KEY>"
```

兼容模式还需要 `auth.json`：

```json
{ "OPENAI_API_KEY": "<YOUR_API_KEY>" }
```

使用 API Key 模式时不需要 `auth.json`。

## 验证

完全退出 Codex，再重新启动并新建任务。旧会话不会重新读取配置。

## WebSocket

如果分组和账号支持 WebSocket，可在密钥页选择“Codex CLI (WebSocket)”。使用 WS 时不要保留 `supports_websockets = false`。
