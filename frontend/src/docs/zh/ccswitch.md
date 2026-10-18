# CC Switch

CC Switch 是桌面端的供应商切换器，支持 Claude Code、Codex、Gemini CLI、Grok Build 等客户端。它只改写客户端的地址、Key 和模型，不会覆盖你已有的 MCP、Skills、Hook 和注释。

## 推荐：从密钥页导入

1. 打开 [密钥页]({{keysUrl}})。
2. 找到目标 Key，点击“导入到 CCS”。
3. 在 CC Switch 中确认供应商名称、Endpoint、API Key 和模型。
4. 选择“直连”模式并启用，然后重启对应客户端。

“导入到 CCS”使用 `ccswitch://v1/import` Deep Link，写入的是：

- Claude Code：`{{baseUrl}}`
- Codex：`{{baseUrl}}`（CC Switch 会自行拼接 OpenAI 兼容路径）
- Gemini CLI：`{{baseUrl}}`
- Grok Build：`{{openaiBaseUrl}}`
- Antigravity：`{{baseUrl}}/antigravity`

## 手动添加

在 CC Switch 中选择工具 → 添加供应商 → 自定义配置，填写：

| 项 | 值 |
| --- | --- |
| 名称 | 任意，例如 `Sub2API Plus` |
| Endpoint / Base URL | 按上面的客户端选择 |
| API Key | 在 [密钥页]({{keysUrl}}) 创建 |
| Model | `YOUR_MODEL_ID` |
| 配置格式 | JSON（Claude / Codex / Gemini / Grok） |

如果 CC Switch 要求填写“模型”，使用当前 Key 分组里的模型 ID；不要填官方模型名。

## 配置文件位置

<div class="docs-os-line" data-os="unix">macOS / Linux：<code>~/.cc-switch/config.json</code></div>

<div class="docs-os-line" data-os="cmd">Windows CMD：<code>%USERPROFILE%\.cc-switch\config.json</code></div>

<div class="docs-os-line" data-os="powershell">Windows PowerShell：<code>%USERPROFILE%\.cc-switch\config.json</code></div>

CC Switch 会把供应商同步写入各客户端自己的配置文件。不要同时手动修改同一份客户端配置，否则切换时可能互相覆盖。CC Switch 第一次改写前会备份原配置。

## 注意

- 本文只描述“直连”模式：请求直接发到本站。
- 如果启用 CC Switch 的“本地路由”，客户端 Base URL 要指向 CC Switch 的本地端口，而不是本站地址；此时应以 CC Switch 的路由面板为准。
- “导入到 CCS”按钮可能被管理员在 **站点设置 → 隐藏 CCS 导入按钮** 中隐藏；按钮不存在时按上面的手动方式添加。
- CC Switch 是第三方工具，版本和菜单可能变化；遇到差异以 CC Switch 官方文档为准。

官方项目：https://github.com/farion1231/cc-switch
