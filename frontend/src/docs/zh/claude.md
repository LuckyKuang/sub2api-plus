# Claude Code

Claude Code 使用 Anthropic Messages 协议，Base URL 填 `{{baseUrl}}`，认证用 `ANTHROPIC_AUTH_TOKEN`。

## 配置文件

<div class="docs-os-line" data-os="unix">macOS / Linux：<code>~/.claude/settings.json</code></div>

<div class="docs-os-line" data-os="cmd">Windows CMD：<code>%USERPROFILE%\.claude\settings.json</code></div>

<div class="docs-os-line" data-os="powershell">Windows PowerShell：<code>%USERPROFILE%\.claude\settings.json</code></div>

```json
{
  "$schema": "https://json.schemastore.org/claude-code-settings.json",
  "env": {
    "ANTHROPIC_BASE_URL": "{{baseUrl}}",
    "ANTHROPIC_AUTH_TOKEN": "<YOUR_API_KEY>",
    "CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC": "1"
  }
}
```

## 或使用环境变量

```bash os=unix
export ANTHROPIC_BASE_URL="{{baseUrl}}"
export ANTHROPIC_AUTH_TOKEN="<YOUR_API_KEY>"
export CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC=1
```

```bat os=cmd
set ANTHROPIC_BASE_URL={{baseUrl}}
set ANTHROPIC_AUTH_TOKEN=<YOUR_API_KEY>
set CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC=1
```

```powershell os=powershell
$env:ANTHROPIC_BASE_URL="{{baseUrl}}"
$env:ANTHROPIC_AUTH_TOKEN="<YOUR_API_KEY>"
$env:CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC="1"
```

## 模型

`ANTHROPIC_MODEL` 可选。模型必须在当前密钥的分组中；在密钥页的“使用密钥”里可以看到推荐值。

## 注意

- 这里使用 `ANTHROPIC_AUTH_TOKEN`，不要误用 `ANTHROPIC_API_KEY`。
- 如果 Base URL 末尾已经有 `/v1`，不要再重复添加。
- 修改后完全退出并重启 Claude Code。
