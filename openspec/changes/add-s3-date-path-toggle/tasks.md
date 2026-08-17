## 1. Backend

- [x] 1.1 Add persisted and runtime `append_date_path` configuration with
  compatibility-preserving defaults.
- [x] 1.2 Build backup and async-image keys from the base prefix and one
  server-timezone timestamp.
- [x] 1.3 Persist private async-image object keys and use them for ZIP reads.
- [x] 1.4 Cover enabled/disabled paths, defaults, midnight boundaries, split
  backups, and exact-key downloads with focused Go tests.

## 2. Frontend

- [x] 2.1 Add typed backup and image date-path fields and defaults.
- [x] 2.2 Add adjacent toggles and effective key-shape previews.
- [x] 2.3 Keep English and Chinese locale keys aligned and add focused Vitest
  coverage.

## 3. Documentation and verification

- [x] 3.1 Document the runtime image-storage setting and environment binding.
- [x] 3.2 Update async-image object-key examples and timezone behavior.
- [x] 3.3 Run strict OpenSpec validation and focused backend/frontend checks.
