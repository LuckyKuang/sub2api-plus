# DeepSeek · DSH Desktop

DSH（DeepSeek Harness）是 DeepSeek 官方的 Agent Harness，Desktop 和 Web UI 共用模型配置。官方推荐的接入方式是 **Settings → Models → Add model provider → Custom model API**，不是 `OPENAI_BASE_URL` 环境变量。

官方文档：https://deepseek-harness.github.io/deepseek-harness/guide/providers

## 通过界面添加

1. 打开 DSH，进入 **Settings → Models**。
2. 点击 **Add model provider**，切换到 **Custom model API**。
3. 填写：

| 项 | 值 |
| --- | --- |
| Provider ID | `sub2api`（小写，保存后不可改名） |
| Display name | `Sub2API Plus` |
| Base URL | `{{openaiBaseUrl}}` |
| API protocol | `OpenAI Responses` |
| API Key | 在 [密钥页]({{keysUrl}}) 创建 |
| Models | `YOUR_MODEL_ID` |

4. 保存后，在模型选择器中选择该模型。

## 协议选择

- Responses 分组：`Base URL = {{openaiBaseUrl}}`，协议 `OpenAI Responses`。
- Chat Completions 分组：`Base URL = {{openaiBaseUrl}}`，协议 `OpenAI Chat Completions`。
- Claude / Anthropic 分组：`Base URL = {{baseUrl}}`，协议 `Anthropic Messages`。
- 一个 provider 只能选一种协议；需要同时使用两种协议时，建两个 Provider ID，例如 `sub2api-responses` 和 `sub2api-anthropic`。

## 配置文件

<div class="docs-os-line" data-os="unix">macOS / Linux：<code>~/.dsh/profiles/web/cordis.patch.yml</code></div>

<div class="docs-os-line" data-os="cmd">Windows CMD：<code>%USERPROFILE%\.dsh\profiles\web\cordis.patch.yml</code></div>

<div class="docs-os-line" data-os="powershell">Windows PowerShell：<code>%USERPROFILE%\.dsh\profiles\web\cordis.patch.yml</code></div>

高级配置可以直接编辑该文件；适配器会在下一次请求时重新读取，无需重启：

```yaml
- id: llm-pi-ai
  config:
    providers:
      sub2api:
        apiKeyEnv: SUB2API_API_KEY
        api: openai-responses
        baseURL: "{{openaiBaseUrl}}"
        models:
          - id: YOUR_MODEL_ID
```

使用 `apiKeyEnv` 时，需要在启动 DSH 的环境中设置 `SUB2API_API_KEY`；也可以继续使用界面保存的凭据。

## 说明

- DSH 默认数据目录是 `~/.dsh`；Windows 为 `%USERPROFILE%\.dsh`。
- 官方文档明确 Provider ID 一旦保存不可重命名；需要改名时请新建 Provider 再删除旧的。
- 模型 ID 以分组和密钥页为准。
