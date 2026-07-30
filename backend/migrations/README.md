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

Runner implementation:
`backend/internal/repository/migrations_runner.go`.
