# GLM · ZCode

ZCode 是智谱官方的图形化 Agent 客户端。官方配置入口是 **Settings → Model Providers**，不是 shell 环境变量。接入本站时，把 OpenAI 和 Anthropic Base URL 指向本站即可。

官方文档：https://docs.z.ai/devpack/tool/zcode

## 配置

1. 打开 ZCode，进入左下角 **Settings**。
2. 在左侧选择 **Model Providers**。
3. 在 **Z.ai Open Platform** 区域填入 API Key。
4. 手动修改 Base URL：

| 协议 | Base URL |
| --- | --- |
| OpenAI 兼容 | `{{openaiBaseUrl}}` |
| Anthropic / Claude 兼容 | `{{baseUrl}}` |

5. 保存后，在模型切换菜单中选择 `YOUR_MODEL_ID`。

## 说明

- ZCode 是图形客户端，macOS / Windows / Linux 的配置步骤相同，不存在 CMD / PowerShell 的 shell 差异。
- 不要把它和后台的“ZCode 账号关联”混淆：账号关联让网关获得上游凭证；客户端连接使用控制台 API Key。
- 官方文档中的 `https://api.z.ai/api/coding/paas/v4` 和 `https://api.z.ai/api/anthropic` 是智谱官方地址；接入本站时替换为上面的 Base URL。
- 如果 ZCode 在 Base URL 后自动补 `/v1`，把 OpenAI Base URL 改为 `{{baseUrl}}`，避免出现 `/v1/v1`。
- 模型 ID 以分组和密钥页为准。
