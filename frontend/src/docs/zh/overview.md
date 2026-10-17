# 快速开始

本站文档按“出站客户端”组织。每个客户端对应一套协议和配置；选错协议时，最常见的表现是 404、401 或模型不存在。

## 1. 创建 API 密钥

打开 [密钥页]({{keysUrl}})，创建一个密钥，并确认它已经分配分组。密钥只在创建时显示一次。

## 2. 确认 Base URL

当前控制台的 API 地址是：

`{{baseUrl}}`

- OpenAI 兼容客户端：`{{openaiBaseUrl}}`
- Anthropic / Claude Code：`{{baseUrl}}`
- Gemini 原生：`{{baseUrl}}`

如果管理员在“站点设置”里配置了 API 地址，以上以控制台显示为准。不要在地址末尾重复添加 `/v1`。

## 3. 选择模型

模型 ID 由分组和渠道决定，不能在文档里写死。打开 [模型广场]({{modelPlazaUrl}}) 或密钥页的“使用密钥”查看当前可用模型。下文示例中的 `YOUR_MODEL_ID` 都需要替换成实际模型。

## 4. 发送第一个请求

```bash
curl {{openaiBaseUrl}}/chat/completions \
  -H "Authorization: Bearer <YOUR_API_KEY>" \
  -H "Content-Type: application/json" \
  -d '{"model":"YOUR_MODEL_ID","messages":[{"role":"user","content":"Hello"}]}'
```

- 401：密钥无效、被禁用，或没有带 `Bearer`。
- 404：Base URL 或协议不匹配。
- 模型不存在：当前密钥的分组没有该模型。

## 下一步

从左侧选择你的客户端。Claude Code、Codex、Gemini CLI 和 OpenCode 也可以直接在密钥页点击“使用密钥”，生成与当前密钥匹配的配置。
