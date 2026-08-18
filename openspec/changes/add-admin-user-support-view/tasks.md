# 1. Contracts and backend safety

- [x] 1.1 Add safe administrator API-key summary DTOs and migrate the existing
  administrator user-key list to omit plaintext credentials.
- [x] 1.2 Add administrator support query services and GET-only routes with
  explicit actor/target audit metadata and non-deleted target validation.
- [x] 1.3 Extend asynchronous-image history with read-only list/detail queries
  by target user while preserving existing API-key owner scoping.
- [x] 1.4 Cover administrator authorization, disabled/deleted targets,
  credential omission, method restrictions, audit identity, and side-effect-free
  asynchronous-image reads with focused Go tests.

# 2. Frontend support mode

- [x] 2.1 Add the searchable administrator account selector and route-based
  self-versus-target mode without changing authentication state.
- [x] 2.2 Preserve all existing self routes and operations, and add a persistent
  read-only support banner and support-only navigation for different targets.
- [x] 2.3 Add API-key summary and asynchronous-image history support pages with
  no credential, copy, export, create, update, delete, retry, or download paths.
- [x] 2.4 Add the remaining safe support pages using read-only administrator
  queries, and omit purchase, redemption, affiliate transfer, batch-image, and
  account-security workflows.
- [x] 2.5 Keep English and Chinese locale keys aligned and add focused router,
  selector, API, and page Vitest coverage.
- [x] 2.6 Reject malformed target IDs, discard stale target/resource responses,
  revalidate cached targets, and avoid subscription enrichment in the account
  selector's all-user query.

# 3. Production hardening

- [x] 3.1 Return real target usage summaries with timezone-aware calendar
  periods instead of the legacy zero-value administrator placeholder.
- [x] 3.2 Mark all validated support responses `no-store` and cover target
  request races, invalid IDs, and usage boundaries with focused tests.

# 4. Verification

- [ ] 4.1 Run focused backend tests, frontend tests, lint, typecheck, locale
  parity, strict OpenSpec validation, and `git diff --check`.
- [ ] 4.2 Verify self CRUD and non-self read-only behavior at desktop, collapsed
  sidebar, and mobile widths with available browser tooling.

Verification status: relevant backend package tests, the complete frontend
Vitest suite, focused support tests, lint, typecheck, locale parity, production
build, `go mod tidy -diff`, and `git diff --check` pass. Strict OpenSpec
validation is pending because the CLI is unavailable in this environment.
Authenticated desktop, collapsed-sidebar, and mobile visual verification is
also pending.
