# Design: Usage alert (multi-channel)

## Storage (accounts.extra)

| Key | Type | Notes |
|-----|------|-------|
| `usage_alert_rules` | array | List of rules (max 20) |

Each rule:

| Field | Type | Notes |
|-------|------|-------|
| `id` | string | Stable id; generated on create |
| `enabled` | bool | Independent per rule; multiple can be on |
| `channel` | string | `wecom` \| `feishu` \| `custom` |
| `webhook_url` | string | Full HTTPS URL; shown to admins (not redacted) |
| `cron_expression` | string | Standard 5-field cron |
| `force_probe` | bool | When true, GetUsage(force=true) |
| `threshold_enabled` | bool | Default false |
| `threshold_percent` | int | 1–99 when threshold enabled |
| `next_run_at` / `last_run_at` / `last_error` | meta | Per-rule |

Legacy single-rule keys `wecom_usage_alert_*` are migrated on read and cleared on save.

## Channels

- **wecom**: markdown `msgtype` to `qyapi.weixin.qq.com/cgi-bin/webhook/send`
- **feishu**: text `msg_type` to Feishu/Lark bot hook URL
- **custom**: JSON body with `title`, `markdown`, account/threshold fields to any HTTPS URL

## Threshold

- Off: every due cron tick sends a **用量窗口报告**
- On: send only when max window utilization ≥ threshold; title **用量阈值告警（≥N%）**
- Test send always ignores the threshold gate

## Runtime

1. Runner every minute
2. `ListDueUsageAlertAccounts` loads candidate accounts (new rules or legacy)
3. For each due enabled rule: GetUsage → optional threshold check → POST webhook → advance that rule's `next_run_at`
4. Failures set per-rule `last_error` and still advance schedule

## API

- `GET/PUT /admin/accounts/:id/usage-alert`
- `POST /admin/accounts/:id/usage-alert/test` body optional `{ rule_id, rule }`
- Legacy `/wecom-usage-alert` routes alias the same handlers
