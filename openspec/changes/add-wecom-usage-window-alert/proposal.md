# Add WeCom usage-window alert

## Problem

Admins need periodic enterprise WeChat (WeCom group bot) reports of an
account's upstream usage windows (5h / 7d) without relying on browser sessions
or admin JWT.

## Proposal

Add per-account WeCom usage-window alerting:

- Store `enabled`, webhook URL, cron, and run metadata in `accounts.extra`
- Backend runner ticks every minute, loads due accounts, calls
  `AccountUsageService.GetUsage` in-process, posts markdown to the WeCom bot
- Admin APIs for get/update/test; webhook URL is returned in full (by design)

## Non-goals

- Threshold-only alerts (can be added later)
- Global WeCom settings
- DingTalk / Slack channels
- Encrypting or redacting the webhook URL

## Impact

- Persistent data: new Extra keys on accounts
- Public admin API: `/admin/accounts/:id/wecom-usage-alert`
- Background runner with Start/Stop lifecycle
