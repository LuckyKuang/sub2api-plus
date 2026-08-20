## 1. Contract and persistence

- [x] 1.1 Specify explicit mode persistence and the `device` default.
- [x] 1.2 Add application-level normalization for account writes.
- [x] 1.3 Backfill legacy rows and enforce the database write invariant.

## 2. Administration UI

- [x] 2.1 Make create, edit, and bulk controls default to and submit `device`.
- [x] 2.2 Update English and Chinese labels and descriptions.

## 3. Verification

- [x] 3.1 Cover runtime fallback, persistence normalization, and migration behavior.
- [x] 3.2 Cover create, edit, and bulk payload semantics.
- [x] 3.3 Run focused frontend, locale, type, lint, and migration filename checks.
- [x] 3.4 Run backend Go formatting, unit tests, and migration integration tests
  when the required Go 1.26.6 and PostgreSQL toolchain is available.
