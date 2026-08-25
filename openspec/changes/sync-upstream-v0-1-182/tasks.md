## 1. Baseline and merge

- [x] 1.1 Verify annotated tag `v0.1.182` at
      `5a7d469622911a6b1291a692376df5fa03f9ac2e`.
- [x] 1.2 Create `release/0.1.182-custom.001` from `origin/main`.
- [x] 1.3 Carry forward the local v0.1.181 Plus integration with an explicit
      no-ff merge, then merge official v0.1.182 with a separate no-ff merge.

## 2. Conflict resolution

- [x] 2.1 Retain Plus README distribution links and section markers.
- [x] 2.2 Compose official Responses Lite and image-prompt fixes with Plus
      Codex identity, WebSocket audit, session, and prompt-cache behavior.
- [x] 2.3 Retain published migrations and the Plus 229–233 migration set; do
      not introduce a v0.1.182 migration.

## 3. Metadata and verification

- [x] 3.1 Prepare `0.1.182+custom.001`, the planned `UPSTREAM.md` mapping,
      release notes, and install/rollback examples.
- [ ] 3.2 Run `go mod tidy -diff`, focused Responses Lite/image/WebSocket/Codex
      tests, generated-code and migration checks.
- [ ] 3.3 Run frontend lint, typecheck, locale parity, and relevant Vitest.
- [ ] 3.4 Run release-document and diff checks in the required environment.
- [x] 3.5 Do not create or push tags, Releases, images, or a remote branch.
