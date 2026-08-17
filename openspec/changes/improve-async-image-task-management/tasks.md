## 1. Backend

- [x] 1.1 Extend Redis and history repository interfaces for deletion and
  owner-scoped status lookup.
- [x] 1.2 Implement failed-only deletion with non-disclosing ownership checks
  and recoverable Redis-before-PostgreSQL ordering.
- [x] 1.3 Register versioned and non-versioned DELETE routes and return 204.
- [x] 1.4 Classify all task-management paths as authenticated billing bypasses.
- [x] 1.5 Cover service, repository, handler, routes, and middleware behavior
  with focused Go tests.

## 2. Frontend

- [x] 2.1 Add the typed deletion API and focused request coverage.
- [x] 2.2 Add failed-row confirmation, row-local pending state, success paging,
  detail cleanup, and failure handling.
- [x] 2.3 Replace both native filters with the shared Select component and keep
  responsive dimensions stable.
- [x] 2.4 Keep English and Chinese locale keys aligned and add focused Vitest
  coverage.

## 3. Documentation and verification

- [x] 3.1 Document deletion eligibility, ownership, data removal, and retained
  object-storage behavior.
- [x] 3.2 Run strict OpenSpec validation and focused backend/frontend checks.
- [x] 3.3 Verify filter alignment and delete states at desktop and mobile
  widths with available browser tooling.
