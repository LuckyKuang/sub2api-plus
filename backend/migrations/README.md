# Database Migrations

SQL migrations are embedded in the application and applied automatically at
startup. The runner records each filename and SHA256 checksum in
`schema_migrations`.

## Mandatory Rules

- Existing migration files are immutable: never modify, delete, or rename them.
- Create a new forward-only migration for every database change.
- Revert behavior with a new compensating migration; there are no Down blocks.
- Use the next unique, increasing numeric prefix.
- Keep each migration focused and idempotent where practical.

The repository contains historical duplicate numeric prefixes. They remain
unchanged because filenames and checksums are already deployed. Do not add new
duplicates.

## File Naming

Regular migration:

```text
NNN_description_in_snake_case.sql
```

Non-transactional concurrent-index migration:

```text
NNN_description_in_snake_case_notx.sql
```

The full filename is the migration identity. Files execute in lexicographic
filename order.

## Execution Model

Regular `*.sql` files execute as a single transaction. Do not add executable
Down SQL or transaction-control sections; the runner executes the complete file
as-is.

Example:

```sql
ALTER TABLE usage_logs
    ADD COLUMN IF NOT EXISTS example_column VARCHAR(100);
```

`*_notx.sql` exists only for concurrent index operations. It may contain:

- `CREATE INDEX CONCURRENTLY IF NOT EXISTS ...`
- `DROP INDEX CONCURRENTLY IF EXISTS ...`

It must not contain other DDL/DML or explicit `BEGIN`, `COMMIT`, or `ROLLBACK`.

## Creating a Migration

1. Find the largest existing numeric prefix.
2. Create one file using the next unused number.
3. Write a forward-only change.
4. Add tests for schema, data, compatibility, or migration behavior.
5. Run:

   ```bash
   cd backend
   go test ./migrations ./internal/repository/...
   go test ./...
   ```

6. Verify startup against both a new database and a representative upgraded
   database when the change is operationally significant.

## Checksum Failures

A checksum mismatch means a previously applied migration differs from the
tracked file. The correct recovery is:

1. Restore the exact released migration content.
2. Create a new migration for the intended change.
3. Re-run the migration and repository tests.

Do not update the database checksum manually. The compatibility allowlist in
`internal/repository/migrations_runner.go` is only for explicitly audited
historical incidents and must not be used for routine development.

## Operational Notes

- PostgreSQL advisory locking serializes migrations across application
  instances.
- Applied files with matching checksums are skipped.
- New migrations run before the application begins normal service.
- A failed regular migration rolls back its transaction.
- A failed non-transactional migration requires operator review before retry.

## Domestic identity environment

Migration 276 deletes the superseded Kimi/ZCode OS, kernel and architecture
overrides from global and account identities. They now use the pinned Ubuntu
24.04 environment. Device name/UUID, locale, timezone and unrelated configuration
are preserved. See [outbound identity](../../docs/OUTBOUND_IDENTITY.md).

## Native client family selection

Migration 278 removes retired default mappings and whole foreign identity
candidates from native provider/cloud accounts. Compatible supplier mappings,
correct-family candidates, global profiles/runtime declarations, credentials,
model restrictions and billing settings remain intact. The cleanup is tested
against isolated PostgreSQL, including repeated execution and empty settings.
See [outbound identity](../../docs/OUTBOUND_IDENTITY.md#selection-and-persistence).

## Upgrade Prerequisites

Back up PostgreSQL before upgrading. Replacing the application binary alone
cannot reverse renamed columns or data cleanup; recovery requires the matching
pre-upgrade backup or a reviewed forward compensation. Applied migration files
and checksums remain immutable.

- **Model policies (259–262):** `models_list_config` becomes `model_allowlist`
  in storage and management JSON. Update management clients to the new field.
  Migration 262 trims entries, removes case-insensitive duplicates in order,
  and disables legacy enabled empty/blank lists. It never replaces them with
  `*`. Malformed JSON, non-string entries and non-trailing wildcard patterns
  stop this historical migration; repair the indicated group before retrying.
  Diagnostics contain group IDs, without model names or credentials. Current
  runtime policies support `*` at any position; that does not change migration
  262's one-time validation. See [model admission](../../docs/protocols/OPENAI_RESPONSES.md).
- **Access logs (263):** an installation with pre-existing user rows preserves
  explicit `persist_access_logs` and receives `true` only when that field is
  missing. Fresh databases keep the application default `false`. Unrelated
  runtime-log fields are preserved; malformed configuration needs repair.
  This switch does not disable required security-audit exception logs.
- **Platforms and quotas (261, 266, 267, 273, 277):** platform constraints retain the
  full Plus platform set, including MiniMax, OpenCode and TypeSafe. Migration
  267 removes quota rows whose daily, weekly and monthly limits are all NULL;
  those rows are unlimited. Migration 273 expands both quota and composite
  target constraints without removing existing platforms. Migration 277 adds
  StepFun to those constraints and the legacy probe-provider constraints.
- **Usage and payments (269, 270, 274):** rollout budget units remain a reserved
  usage dimension; affiliate `operation_id` supports idempotent ledger writes.
  Historical payment orders receive `bonus_amount=0`. See [payment behavior](../../docs/PAYMENT.md).
- **Authentication caches:** password-reset links stored in the previous
  plaintext form are rejected; users request new links. Verification codes
  carry forward legacy attempt counters: four attempts permit at most one
  more comparison; five or more permit none. See [authentication](../../docs/AUTHENTICATION.md).

For explicit upstream URL allowlists, add `api.minimax.io` for the international
MiniMax site (`api.minimaxi.com` for China); new defaults do not merge into
existing explicit lists. The system-log cleanup fallback
`ops.cleanup.system_log_retention_days` is 30 and must be positive when cleanup
is enabled; runtime ops settings can override it. See the maintained
[deployment configuration](../../deploy/config.example.yaml).

Runner implementation:
`backend/internal/repository/migrations_runner.go`.

## Local security audit error attribution

Migration 279 corrects reliably identifiable historical local 403 refusals to
request/client attribution with their exact policy or prompt-guard code.
Malformed, ambiguous and provider records remain unchanged. It preserves
bodies, timestamps, business-limit flags and unknown timing and is safe to repeat.
The runtime uses trusted decisions for new records. See
[error diagnostics](../../docs/ERROR_REQUEST_DIAGNOSTICS.md).
