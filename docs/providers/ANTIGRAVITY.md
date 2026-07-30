# Antigravity

Sub2API Plus supports authorized Antigravity accounts for Claude and Gemini
traffic.

## Dedicated Endpoints

| Endpoint | Models |
| --- | --- |
| `/antigravity/v1/messages` | Claude |
| `/antigravity/v1beta/` | Gemini |

For Claude Code-style clients:

```bash
export ANTHROPIC_BASE_URL="https://your-sub2api.example.com/antigravity"
export ANTHROPIC_AUTH_TOKEN="sk-your-sub2api-key"
```

## Hybrid Scheduling

When hybrid scheduling is enabled, the general `/v1/messages` and `/v1beta/`
routes can also select Antigravity accounts.

Anthropic Claude and Antigravity Claude must not be mixed in the same
conversation context. Use separate groups to isolate them.
