# MiniMax Code

MiniMax Code 的 CLI 是 `mcode`。官方通过 `mcode provider add` 添加自定义供应商，适合把本站作为 OpenAI 兼容或 Anthropic 兼容端点接入。

官方文档：https://agent.minimax.io/docs/cli/configuration

## 设置 API Key

```bash os=unix
export MCODE_PROVIDER_API_KEY="<YOUR_API_KEY>"
```

```bat os=cmd
set MCODE_PROVIDER_API_KEY=<YOUR_API_KEY>
```

```powershell os=powershell
$env:MCODE_PROVIDER_API_KEY="<YOUR_API_KEY>"
```

## 添加供应商

```bash
mcode provider add \
  --name sub2api \
  --base-url "{{openaiBaseUrl}}" \
  --api-format openai-responses \
  --model "YOUR_MODEL_ID" \
  --api-key-env MCODE_PROVIDER_API_KEY \
  --use
```

- `--api-format` 可选 `openai-completions`、`openai-responses`、`anthropic-messages`。
- 本站分组是 Responses 时用 `openai-responses`；普通 Chat Completions 用 `openai-completions`；Claude 分组用 `anthropic-messages`（此时 `--base-url` 用 `{{baseUrl}}`）。
- `--use` 会先测试第一个模型再保存；测试失败不会写入配置。

## 验证

```bash
mcode provider list
mcode provider test sub2api --model "YOUR_MODEL_ID"
```

进入 `mcode` 后用 `/provider` 选择供应商，或用 `/model` 切换模型。

## 说明

- OAuth / Token Plan 账号与自定义供应商是两条路径；客户端接入本站时使用上面的 API Key 供应商。
- MiniMax Code 的用户数据目录默认是 `~/.minimax`；Windows 为 `%USERPROFILE%\.minimax`。
- 不要把真实 API Key 写进仓库。
