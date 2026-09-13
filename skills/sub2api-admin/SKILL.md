---
name: sub2api-admin
description: >-
  Query and manage a specified Sub2API instance through its admin API. Use for
  requested instance operations such as account maintenance or redeem-code
  management, not ordinary repository development or documentation review.
---

# Sub2API Admin

Use this skill when the task calls for operating a deployed instance. Mentioning
Sub2API, editing account-management code, or reviewing API documentation does
not authorize an instance request.

Use the bundled CLI instead of ad hoc `curl`. Run examples from this skill directory.

```bash
export SUB2API_BASE_URL='https://your-sub2api-host'
export SUB2API_ADMIN_API_KEY='<admin api key>'
# Or, when the deployment uses admin JWT login instead of an admin API key:
# export SUB2API_JWT='<admin access_token>'
node scripts/sub2api-admin.js accounts list
```

Read the relevant section of [references/admin-cli.md](references/admin-cli.md)
for the requested command and payload examples.

## Workflow

1. Resolve the intended instance from the task and `SUB2API_BASE_URL`; reuse either `SUB2API_ADMIN_API_KEY` or `SUB2API_JWT` from the environment. Ask only when the instance is missing or ambiguous.
2. Run read-only commands first: `accounts list`, `accounts get <id>`, `groups all`, or `proxies all`.
3. Before destructive or bulk writes, print the target account names and IDs.
4. Execute a write only when the user has authorized that operation on the identified targets. Reuse authorization already given in the conversation; otherwise present the concrete operation and targets for approval before writing. Clear targets or available credentials alone do not authorize a write.
5. Run a follow-up read command to verify the result.

## Common Commands

```bash
node scripts/sub2api-admin.js accounts list --page-size 20
node scripts/sub2api-admin.js accounts get 40
node scripts/sub2api-admin.js accounts usage 40
node scripts/sub2api-admin.js accounts set-schedulable 40 true
node scripts/sub2api-admin.js accounts bulk-update --ids 40,39 --json '{"concurrency":10}'
node scripts/sub2api-admin.js redeem-codes list --page-size 20
node scripts/sub2api-admin.js redeem-codes generate --json '{"count":1,"type":"balance","value":10}' --idempotency-key redeem-$(date +%s)
node scripts/sub2api-admin.js redeem-codes create-and-redeem --json '{"code":"order_123","type":"balance","value":10,"user_id":123}' --idempotency-key order-123
node scripts/sub2api-admin.js error-rules list
node scripts/sub2api-admin.js tls-profiles list
```

## Safety Notes

- Authentication uses `x-api-key` from `SUB2API_ADMIN_API_KEY` first, then falls back to `Authorization: Bearer <jwt>` from `SUB2API_JWT`.
- If the API returns `INVALID_ADMIN_KEY`, ask the user to regenerate the admin API key. If using JWT, log in as an admin user and copy the `access_token` from `POST /api/v1/auth/login`.
- `accounts export` includes credentials and tokens. Prefer `--file` and avoid printing exports in chat.
- Redeem code create/redeem commands should use `--idempotency-key` for payment or recharge workflows.
- For uncertain or newly added backend APIs, use `api <METHOD> <admin-path>` after a read-only check.
