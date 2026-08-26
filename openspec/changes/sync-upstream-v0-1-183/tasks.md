## 1. Baseline and merge

- [x] 1.1 Verify annotated tag `v0.1.183` at
      `e8cb019fabf8b55199436229044cbf9aa7a82564`.
- [x] 1.2 Create `release/0.1.183-custom.001` from
      `release/0.1.182-custom.001`.
- [x] 1.3 Merge official v0.1.183 into the 183-custom branch.

## 2. Conflict resolution

- [x] 2.1 Set embedded version to `0.1.183+custom.001`.
- [x] 2.2 Keep Plus module paths and adopt official email-rebind / Antigravity
      test imports.
- [x] 2.3 Keep `codexSessionIDHeader` and Plus sticky bind helper; add official
      `stickySpillover`.
- [x] 2.4 Adopt official OAuth 429 classification helpers and retain Plus
      request-body / proxy-buffer failover reasons.
- [x] 2.5 Retain published migrations and the Plus 229–233 migration set; do
      not introduce a v0.1.183 migration.

## 3. Metadata and verification

- [x] 3.1 Prepare `0.1.183+custom.001`, the planned `UPSTREAM.md` mapping,
      release notes, and install/rollback examples.
- [x] 3.2 Run focused scheduling, failover, email-bind, Antigravity, and
      WebSocket session-header tests.
- [ ] 3.3 Run remaining release-document, release-policy, and Debian WSL
      validation before publish.
- [x] 3.4 Do not create or push tags, Releases, images, or a remote branch.
