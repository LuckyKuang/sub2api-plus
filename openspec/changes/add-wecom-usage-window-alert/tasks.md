## 1. Backend service
- [x] Multi-rule Extra key `usage_alert_rules` + channel/threshold fields
- [x] Legacy `wecom_usage_alert_*` read migration
- [x] WeCom / Feishu / custom webhook clients
- [x] Threshold gate (1–99) with title change
- [x] Get/Update/Test + RunDue runner (multiple rules per account)
- [x] Repo ListDueUsageAlertAccounts
- [x] Routes `/usage-alert` (+ legacy `/wecom-usage-alert` aliases)

## 2. Frontend
- [x] API helpers (`usage-alert`)
- [x] UsageAlertPanel multi-rule dialog
- [x] AccountActionMenu + AccountsView entry（用量告警）
- [x] zh/en locale keys

## 3. Tests
- [x] Channel URL validation / threshold normalize / multi-rule / webhook unit tests
