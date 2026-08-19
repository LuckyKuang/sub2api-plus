## 1. Specification

- [x] 1.1 Define the shared one-year setting bound.
- [x] 1.2 Define manual-only refresh behavior.
- [x] 1.3 Define permanent and concurrency-safe quick manual blocks.

## 2. Backend

- [x] 2.0 Fix the threshold transaction's inconsistent source-IP parameter type.
- [x] 2.1 Update setting validation and persisted-setting parsing.
- [x] 2.2 Change the quick-block repository contract to omit duration.
- [x] 2.3 Guarantee an exact permanent manual rule in the repository transaction.
- [x] 2.4 Update service, repository, handler, middleware, and integration tests.

## 3. Frontend

- [x] 3.1 Update the failure-window input maximum.
- [x] 3.2 Remove timer and visibility refresh behavior.
- [x] 3.3 Update permanent-block confirmation and availability checks.
- [x] 3.4 Update English and Chinese locale text and focused Vitest coverage.

## 4. Verification

- [x] 4.1 Run strict OpenSpec validation.
- [x] 4.2 Run focused backend checks in the repository-supported environment.
- [x] 4.3 Run focused frontend tests, typecheck, and lint.
- [x] 4.4 Review the final diff and whitespace checks.
