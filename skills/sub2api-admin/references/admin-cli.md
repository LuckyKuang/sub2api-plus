# Sub2API Admin Reference

## Environment

```bash
export SUB2API_BASE_URL='https://your-sub2api-host'
export SUB2API_ADMIN_API_KEY='<admin api key>'
# 或者，未配置管理员 API Key 时使用管理员 JWT：
# export SUB2API_JWT='<admin access_token>'
```

普通命令优先使用 `SUB2API_ADMIN_API_KEY` 发送 `x-api-key`，未设置时使用 `SUB2API_JWT` 发送 `Authorization: Bearer <jwt>`。账号导出必须使用 JWT，且要求取消设置管理员 API Key。如果返回 `INVALID_ADMIN_KEY`，重新生成管理员 API Key；如果使用 JWT，先完成管理员登录（启用 TOTP 时还需完成二次验证），再使用最终响应的 `data.access_token`。密码登录入口为：

```bash
curl -sS "$SUB2API_BASE_URL/api/v1/auth/login" \
  -H 'Content-Type: application/json' \
  -d '{"email":"admin@example.com","password":"your-password"}'
```

## CLI

以下命令都假设当前目录是这个 skill 目录。

```bash
node scripts/sub2api-admin.js <command>
```

## Accounts

### 只读

```bash
node scripts/sub2api-admin.js accounts list --page-size 20
node scripts/sub2api-admin.js accounts list --search outlook --platform openai --type oauth --status active
node scripts/sub2api-admin.js accounts get 40
node scripts/sub2api-admin.js accounts usage 40
node scripts/sub2api-admin.js accounts stats 40 --days 30
node scripts/sub2api-admin.js accounts today-stats 40
node scripts/sub2api-admin.js accounts batch-today-stats --ids 40,39
node scripts/sub2api-admin.js accounts models 40
node scripts/sub2api-admin.js accounts temp-unschedulable 40
node scripts/sub2api-admin.js accounts antigravity-default-model-mapping
```

### Account export

导出使用管理员 JWT。先将 `SUB2API_JWT` 设置为已完成登录的管理员 access token，
并取消设置 `SUB2API_ADMIN_API_KEY`；同时设置两者也会被命令拒绝，避免改变其他命令的鉴权优先级。

在仓库外的受保护本地文件中保存确认信息，例如 `/secure/account-export-confirmation.json`：

```json
{
  "password": "your-administrator-password",
  "totp_code": "012345"
}
```

`password` 必填，发送时保留原始值；`totp_code` 必须为字符串以保留前导零，未启用
step-up 时可以省略。确认文件由用户准备，限制为本人可读写，使用后删除；不要把内容
放入命令行参数、查询 URL、聊天或 Git。

```bash
unset SUB2API_ADMIN_API_KEY
node scripts/sub2api-admin.js accounts export \
  --confirmation-file /secure/account-export-confirmation.json \
  --ids 40,39 --include-proxies false --file /secure/accounts-export.json

node scripts/sub2api-admin.js accounts export \
  --confirmation-file /secure/account-export-confirmation.json \
  --platform openai --type oauth --file /secure/accounts-export.json
```

`--ids` 与筛选条件互斥；均未指定时导出所有账号。支持 `--platform`、`--type`、
`--status`、`--group`、`--search`、`--privacy-mode`、`--sort-by` 和 `--sort-order`。
`--group` 使用正整数分组 ID 或 `ungrouped`，不接受分组名称；列表接口遵循相同约定。
账号 ID 必须通过 `--ids` 指定，多余的位置参数会在请求前被拒绝，避免误导出全部账号。
`--include-proxies` 发送布尔值；省略时使用后端默认值。

当前接口为 `GET /api/v1/admin/accounts/export/requirements` 和
`POST /api/v1/admin/accounts/export`。POST 必须使用管理员 JWT 会话，提交管理员
`password`，并在启用 step-up 时提交 `totp_code`；管理员 API Key 不能授权导出。
导出范围通过请求体的 `ids` 或 `filters` 指定，`include_proxies` 控制代理导出。
命令先查询要求；查询失败、响应格式不正确或缺少所需 TOTP 时停止，不提交导出。
POST 仍由服务器重新检查密码、当前 TOTP 策略和审计要求；失败后不自动重试或改用旧接口。
以 [后端导出请求和校验](../../../backend/internal/handler/admin/account_data.go)
及 [前端调用](../../../frontend/src/api/admin/accounts.ts) 为准。

`--file` 在目标目录的私有临时目录中写入权限为 `0600` 的完整备份，成功后原子替换
目标路径。覆盖时不会跟随符号链接或改写其他硬链接；写入或替换失败保留旧备份，
并清理临时文件。
终端只输出文件路径、账号数 `count` 和排除的影子账号数 `skipped_shadows`。
影子账号的独立配置不在此备份中，恢复时需按后端契约重建和调整。
未指定 `--file` 时仍会将完整备份输出到 stdout；备份含凭据和 token，不要输出到聊天。

[导出契约回归](../tests/test_sub2api_admin.cjs)使用本地模拟服务，不访问实际实例。
按 [CONTRIBUTING.md](../../../CONTRIBUTING.md#development-checks) 在平台验证容器中执行。

### 单账号写入

```bash
node scripts/sub2api-admin.js accounts create --file account.json
node scripts/sub2api-admin.js accounts update 40 --json '{"concurrency":20}'
node scripts/sub2api-admin.js accounts set-status 40 active
node scripts/sub2api-admin.js accounts set-schedulable 40 true
node scripts/sub2api-admin.js accounts clear-error 40
node scripts/sub2api-admin.js accounts clear-rate-limit 40
node scripts/sub2api-admin.js accounts recover-state 40
node scripts/sub2api-admin.js accounts reset-quota 40
node scripts/sub2api-admin.js accounts refresh 40
node scripts/sub2api-admin.js accounts test 40
node scripts/sub2api-admin.js accounts sync-models 40
node scripts/sub2api-admin.js accounts apply-oauth 40 --file credentials.json
node scripts/sub2api-admin.js accounts reset-temp-unschedulable 40
```

### 删除与清理

删除前先列出目标账号名和 ID。

```bash
node scripts/sub2api-admin.js accounts delete 25
node scripts/sub2api-admin.js accounts keep-only --name 'target@example.com'
```

### 批量写入

```bash
node scripts/sub2api-admin.js accounts batch-create --file accounts.json
node scripts/sub2api-admin.js accounts batch-update-credentials --file payload.json
node scripts/sub2api-admin.js accounts bulk-update --ids 40,39 --json '{"concurrency":10,"priority":2}'
node scripts/sub2api-admin.js accounts batch-refresh --ids 40,39
node scripts/sub2api-admin.js accounts batch-clear-error --ids 40,39
```

`bulk-update` 可覆盖页面“批量更新”的字段，payload 由后台表单字段决定，例如 `base_url`、`model_mapping`、`group_ids`、`proxy_id`、`concurrency`、`priority`、`rate_multiplier`、`status`、`compact_mode` 等。更新前先用 `accounts get <id>` 确认字段名。

### 导入

通用后台导入：

```bash
node scripts/sub2api-admin.js accounts import-data --file accounts-export.json
node scripts/sub2api-admin.js accounts import-codex-session --file payload.json
```

CRS 同步：

```bash
node scripts/sub2api-admin.js accounts crs-preview --file payload.json
node scripts/sub2api-admin.js accounts crs-sync --file payload.json
```

旧版 JSON 导入仍可用，会把模板账号的配置复制给导入账号：

```bash
node scripts/sub2api-admin.js accounts import-json \
  --file /path/accounts.json \
  --template-name 'template@example.com' \
  --dry-run
```

复制字段：

- `concurrency`
- `priority`
- `group_ids`
- `credentials.model_mapping`

## Groups And Proxies

```bash
node scripts/sub2api-admin.js groups all
node scripts/sub2api-admin.js proxies all
```

## Redeem Codes

兑换码类型包括 `balance`、`concurrency`、`subscription`、`invitation`。状态常用 `unused`、`used`、`expired`。

### 只读

```bash
node scripts/sub2api-admin.js redeem-codes list --page-size 20
node scripts/sub2api-admin.js redeem-codes list --type balance --status unused --search user@example.com
node scripts/sub2api-admin.js redeem-codes get 123
node scripts/sub2api-admin.js redeem-codes stats
node scripts/sub2api-admin.js redeem-codes export --file redeem-codes.csv
```

### 生成兑换码

```bash
node scripts/sub2api-admin.js redeem-codes generate \
  --json '{"count":1,"type":"balance","value":10}' \
  --idempotency-key "redeem-generate-$(date +%s)"
```

订阅兑换码需要 `group_id` 和非零 `validity_days`：

```bash
node scripts/sub2api-admin.js redeem-codes generate \
  --json '{"count":1,"type":"subscription","value":0,"group_id":2,"validity_days":30}' \
  --idempotency-key "redeem-subscription-$(date +%s)"
```

### 创建并兑换

用于支付回调或人工充值，一步完成创建兑换码并兑换到用户。生产流程必须传稳定的 `--idempotency-key`。

```bash
node scripts/sub2api-admin.js redeem-codes create-and-redeem \
  --json '{"code":"order_123","type":"balance","value":10,"user_id":123,"notes":"manual recharge"}' \
  --idempotency-key order-123
```

### 修改与清理

写入前先 `list` 或 `get` 核对目标 ID。

```bash
node scripts/sub2api-admin.js redeem-codes batch-update --ids 123,124 --json '{"notes":"campaign A"}'
node scripts/sub2api-admin.js redeem-codes expire 123
node scripts/sub2api-admin.js redeem-codes delete 123
node scripts/sub2api-admin.js redeem-codes batch-delete --ids 123,124
```

## Error Rules And TLS Profiles

对应账号页顶部“错误透传规则”和“TLS 指纹模板”。

```bash
node scripts/sub2api-admin.js error-rules list
node scripts/sub2api-admin.js error-rules get 1
node scripts/sub2api-admin.js error-rules create --file rule.json
node scripts/sub2api-admin.js error-rules update 1 --json '{"enabled":true}'
node scripts/sub2api-admin.js error-rules toggle 1 false
node scripts/sub2api-admin.js error-rules delete 1

node scripts/sub2api-admin.js tls-profiles list
node scripts/sub2api-admin.js tls-profiles get 1
node scripts/sub2api-admin.js tls-profiles create --file profile.json
node scripts/sub2api-admin.js tls-profiles update 1 --file profile.json
node scripts/sub2api-admin.js tls-profiles delete 1
```

## Raw Admin API

未封装或新版本后台接口可用 `api` 直通。路径可写 `/admin/...` 或 `/api/v1/admin/...`。

```bash
node scripts/sub2api-admin.js api GET /admin/groups/all
node scripts/sub2api-admin.js api POST /admin/accounts/bulk-update \
  --json '{"account_ids":[40],"concurrency":10}'
```

## Confirmed Admin Endpoints

- `GET /api/v1/admin/accounts`
- `GET /api/v1/admin/accounts/:id`
- `POST /api/v1/admin/accounts`
- `PUT /api/v1/admin/accounts/:id`
- `DELETE /api/v1/admin/accounts/:id`
- `POST /api/v1/admin/accounts/check-mixed-channel`
- `GET /api/v1/admin/accounts/:id/usage`
- `GET /api/v1/admin/accounts/:id/stats`
- `GET /api/v1/admin/accounts/:id/today-stats`
- `POST /api/v1/admin/accounts/today-stats/batch`
- `POST /api/v1/admin/accounts/:id/schedulable`
- `POST /api/v1/admin/accounts/:id/test`
- `POST /api/v1/admin/accounts/:id/refresh`
- `POST /api/v1/admin/accounts/:id/apply-oauth-credentials`
- `POST /api/v1/admin/accounts/:id/clear-error`
- `POST /api/v1/admin/accounts/:id/clear-rate-limit`
- `POST /api/v1/admin/accounts/:id/recover-state`
- `POST /api/v1/admin/accounts/:id/reset-quota`
- `GET /api/v1/admin/accounts/:id/temp-unschedulable`
- `DELETE /api/v1/admin/accounts/:id/temp-unschedulable`
- `GET /api/v1/admin/accounts/:id/models`
- `POST /api/v1/admin/accounts/:id/models/sync-upstream`
- `POST /api/v1/admin/accounts/batch`
- `POST /api/v1/admin/accounts/batch-update-credentials`
- `POST /api/v1/admin/accounts/bulk-update`
- `POST /api/v1/admin/accounts/batch-refresh`
- `POST /api/v1/admin/accounts/batch-clear-error`
- `GET /api/v1/admin/accounts/export/requirements`
- `POST /api/v1/admin/accounts/export` (JWT + password + conditional TOTP)
- `POST /api/v1/admin/accounts/data`
- `POST /api/v1/admin/accounts/import/codex-session`
- `POST /api/v1/admin/accounts/sync/crs/preview`
- `POST /api/v1/admin/accounts/sync/crs`
- `GET /api/v1/admin/accounts/antigravity/default-model-mapping`
- `GET /api/v1/admin/groups/all`
- `GET /api/v1/admin/proxies/all`
- `GET /api/v1/admin/redeem-codes`
- `GET /api/v1/admin/redeem-codes/export`
- `GET /api/v1/admin/redeem-codes/stats`
- `GET /api/v1/admin/redeem-codes/:id`
- `POST /api/v1/admin/redeem-codes/generate`
- `POST /api/v1/admin/redeem-codes/create-and-redeem`
- `POST /api/v1/admin/redeem-codes/batch-update`
- `POST /api/v1/admin/redeem-codes/batch-delete`
- `POST /api/v1/admin/redeem-codes/:id/expire`
- `DELETE /api/v1/admin/redeem-codes/:id`
- `GET /api/v1/admin/error-passthrough-rules`
- `GET /api/v1/admin/error-passthrough-rules/:id`
- `POST /api/v1/admin/error-passthrough-rules`
- `PUT /api/v1/admin/error-passthrough-rules/:id`
- `DELETE /api/v1/admin/error-passthrough-rules/:id`
- `GET /api/v1/admin/tls-fingerprint-profiles`
- `GET /api/v1/admin/tls-fingerprint-profiles/:id`
- `POST /api/v1/admin/tls-fingerprint-profiles`
- `PUT /api/v1/admin/tls-fingerprint-profiles/:id`
- `DELETE /api/v1/admin/tls-fingerprint-profiles/:id`

## Notes

- 线上写入前先只读核对目标集合。
- 导出结果包含敏感凭据，优先使用 `--file`。
- `PUT /admin/accounts/:id` 和 `bulk-update` 接受宽松请求体，字段名不确定时先用 `accounts get` 或后台页面确认。
