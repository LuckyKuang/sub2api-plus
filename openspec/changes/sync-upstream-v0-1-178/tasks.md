## 1. Baseline and merge

- [x] 1.1 Verify official tag `v0.1.178` at
  `e0c48a19ed794a565e3858662520afe0a1f9f0ba`.
- [x] 1.2 Create `sync/upstream-v0.1.178` from the Plus `main` baseline.
- [x] 1.3 Merge the tag with a no-ff merge and inventory conflicts.

## 2. Conflict resolution

- [x] 2.1 Retain the Plus module path, workflows, release identity, and
  distribution links.
- [x] 2.2 Import channel-monitor quota, CN-provider, and time-pricing schema,
  repository, service, route, UI, and locale changes.
- [x] 2.3 Preserve credential-owner identity precedence and OAuth session
  authorization across HTTP, passthrough, and WebSocket paths.
- [x] 2.4 Compose upstream Codex fingerprint/compaction/turn handling with Plus
  prompt-cache and timing behavior.
- [x] 2.5 Regenerate Ent and Wire output after schema/provider changes.
- [x] 2.6 Keep the latest Plus mode migration at 224, remove the superseded
  upstream seed backfill, and assign the CN-provider constraint migration 228.

## 3. Metadata and tests

- [x] 3.1 Prepare `0.1.178+custom.001`, the planned `UPSTREAM.md` mapping, and
  synchronized install/rollback examples.
- [x] 3.2 Add release notes and this OpenSpec change.
- [x] 3.3 Run `go mod tidy -diff` and verify no official module imports remain.
- [x] 3.4 Run backend focused/full service tests and frontend lint, typecheck,
  and Vitest coverage.

## 4. Verification and handoff

- [x] 4.1 Verify generated-code, migration, locale, release-document, and diff
  checks.
- [x] 4.2 Push the review branch to the personal fork and open the PR.
- [x] 4.3 Do not create or push tags, Releases, or images in this change.
