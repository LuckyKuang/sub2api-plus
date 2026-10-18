# StepFun · Step-Code

Step Code 通过 `~/.stepcode/models.json` 添加自定义模型供应商。官方支持 `openai-completions`、`openai-responses`、`anthropic-messages` 等协议；接入本站时使用 Responses 或 Anthropic Messages。

官方文档：https://platform.stepfun.com/docs/zh/step-code/configuration/models

## 配置文件

<div class="docs-os-line" data-os="unix">macOS / Linux：<code>~/.stepcode/models.json</code></div>

<div class="docs-os-line" data-os="cmd">Windows CMD：<code>%USERPROFILE%\.stepcode\models.json</code></div>

<div class="docs-os-line" data-os="powershell">Windows PowerShell：<code>%USERPROFILE%\.stepcode\models.json</code></div>

## Responses 分组

```json
{
  "providers": {
    "sub2api": {
      "baseUrl": "{{openaiBaseUrl}}",
      "api": "openai-responses",
      "apiKey": "<YOUR_API_KEY>",
      "models": [
        { "id": "YOUR_MODEL_ID" }
      ]
    }
  }
}
```

## Claude / Anthropic 分组

```json
{
  "providers": {
    "sub2api": {
      "baseUrl": "{{baseUrl}}",
      "api": "anthropic-messages",
      "apiKey": "<YOUR_API_KEY>",
      "models": [
        { "id": "YOUR_MODEL_ID" }
      ]
    }
  }
}
```

## 验证

保存后重新打开 `/model` 加载变更，选择 `sub2api/YOUR_MODEL_ID`。命令行也可以运行：

```bash
step --provider sub2api --model YOUR_MODEL_ID
```

## 说明

- `baseUrl` 对 OpenAI 兼容端点必须包含 `/v1`；`{{openaiBaseUrl}}` 已处理。
- `apiKey` 会保存在本地 JSON 中，不要把真实 Key 提交到仓库。
- 不要用 `STEP_API_KEY` / `STEP_BASE_URL` 指向本站；它们属于阶跃官方 Step 平台。
