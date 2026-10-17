# 常见问题

## 401 Unauthorized

- 密钥是否复制完整，是否被删除或禁用。
- 请求头是否使用 `Authorization: Bearer <KEY>`。
- Anthropic 客户端是否把 Key 填到了 OAuth token 字段。
- 是否误用了上游账号的 Key，而不是控制台创建的 Key。

## 404 Not Found

- Base URL 是否多了或少了 `/v1`。
- Anthropic 客户端是否误用了 OpenAI 的 `/chat/completions`。
- Gemini 客户端是否应该使用 `/v1beta`。
- 反向代理是否改写了路径。

## 模型不存在

- 模型 ID 是否属于当前密钥的分组。
- 模型是否在模型广场可见。
- 是否把示例里的 `YOUR_MODEL_ID` 原样提交了。

## 配置不生效

- 完全退出客户端再启动，不要只关闭窗口。
- 检查 shell profile 里是否有旧的环境变量覆盖。
- Codex 检查 `wire_api = "responses"`。
- Claude Code 检查使用的是 `ANTHROPIC_AUTH_TOKEN`。
- OpenCode 检查 `baseURL` 是否指向 `{{openaiBaseUrl}}`。

## WebSocket 连接失败

- 分组或账号未启用 WebSocket。
- 临时改回 HTTP/SSE，确认基础请求可用。
- Codex 检查是否还保留 `supports_websockets = false`。

## 代理/直连

见[网络与代理](/docs/network)。

## 仍然无法解决

联系管理员时，请提供：客户端名称与版本、Base URL、请求时间、错误码和错误信息（不要发送完整 API Key）。
