# Design: WeCom usage-window alert

## Storage (accounts.extra)

| Key | Type | Notes |
|-----|------|-------|
| `wecom_usage_alert_enabled` | bool | Must be true to schedule |
| `wecom_usage_alert_webhook_url` | string | Full WeCom bot URL; shown to admins |
| `wecom_usage_alert_cron` | string | Standard 5-field cron |
| `wecom_usage_alert_force_probe` | bool | When true, GetUsage(force=true) |
| `wecom_usage_alert_next_run_at` | RFC3339 | Computed on save / after run |
| `wecom_usage_alert_last_run_at` | RFC3339 | Last attempt |
| `wecom_usage_alert_last_error` | string | Empty on success |

## Runtime

1. Runner every minute (same cadence as scheduled tests)
2. `ListDueWeComUsageAlertAccounts` selects enabled accounts, filters by `next_run_at`
3. For each account: `GetUsage` → format markdown → POST webhook → update run state
4. Failures are recorded in `last_error` and still advance `next_run_at`

## Auth / permission

- No admin HTTP session: runner calls service layer directly
- Upstream tokens come from account credentials already stored server-side
- WeCom outbound webhook only needs the bot key in the URL

## API

- `GET /admin/accounts/:id/wecom-usage-alert`
- `PUT /admin/accounts/:id/wecom-usage-alert`
- `POST /admin/accounts/:id/wecom-usage-alert/test` (send immediately, does not change schedule)
