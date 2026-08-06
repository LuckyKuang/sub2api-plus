# Add usage alert (multi-channel)

## Problem

Admins need scheduled usage-window reports for an account pushed to chat bots
(WeCom / Feishu / custom HTTPS webhook) without relying on browser sessions.
They also need optional utilization thresholds and multiple concurrent schedules
per account.

## Proposal

Upgrade the former WeCom-only alert into **用量告警 (Usage Alert)**:

- Store multiple rules in `accounts.extra.usage_alert_rules`
- Each rule: channel (`wecom` | `feishu` | `custom`), webhook URL, cron,
  force_probe, optional threshold (1–99), run metadata
- Runner ticks every minute; for each due enabled rule, load usage in-process
  and POST to the configured webhook
- Threshold off: push on every cron tick (report title)
- Threshold on: alert only when max window utilization ≥ threshold (threshold title)
- Admin APIs: `/admin/accounts/:id/usage-alert` (legacy WeCom path kept as alias)
- Webhook URLs returned in full (by design); migrate legacy single WeCom Extra keys on read

## Non-goals

- Global bot settings
- DingTalk / Slack first-class channels (use `custom`)
- Encrypting or redacting webhook URLs

## Impact

- Persistent data: `usage_alert_rules` Extra key (+ legacy WeCom keys cleared on save)
- Public admin API: `/admin/accounts/:id/usage-alert`
- Background runner with Start/Stop lifecycle
