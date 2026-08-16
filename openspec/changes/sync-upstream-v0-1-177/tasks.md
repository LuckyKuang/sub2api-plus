## 1. Baseline and merge

- [x] 1.1 Refresh and verify official tag `v0.1.177` at `073e92d17178a1ccdb0a27017f572f10c9c7ab62`.
- [x] 1.2 Create branch `sync/upstream-v0.1.177` from clean Plus `main`.
- [x] 1.3 Merge the tag with `--no-commit --no-ff` and inventory actual conflicts.

## 2. Conflict resolution

- [x] 2.1 Retain Plus workflows, module path, release identity, and toolchain sources.
- [x] 2.2 Adopt migrations 222/223 and the complete server-timezone rollup lifecycle.
- [x] 2.3 Compose native compaction v2 and legacy compact with Plus path and profit guards.
- [x] 2.4 Integrate owner-normalized turn-state relay at response-header commit points.
- [x] 2.5 Preserve one credential-owner-aware fingerprint staging mechanism and Plus `session` default.
- [x] 2.6 Preserve Plus session policy, outbound identity precedence, quota, and observability behavior.
- [x] 2.7 Adopt official Grok billing/media fixes, account refresh preference, and frontend lock update.
- [x] 2.8 Rewrite all new Go imports to `github.com/LuckyKuang/sub2api-plus`.

## 3. Metadata and tests

- [x] 3.1 Set embedded application version to `0.1.177+custom.001` and add a planned `UPSTREAM.md` mapping.
- [x] 3.2 Cover fingerprint storage states, account failover, HTTP map/raw metadata, and compact exclusions.
- [x] 3.3 Cover native-versus-legacy compaction routing, beta features, probe output, unsafe-path rejection, and profit admission.
- [x] 3.4 Cover turn-state commit timing, owner/shadow normalization, foreign-owner stripping, and failed attempts.
- [x] 3.5 Cover rollup timezone precedence/rebuild, yesterday cost, and invalidation/resynchronization.
- [x] 3.6 Cover frontend create/edit/bulk fingerprint semantics, locale wording/parity, group usage, and refresh preference.

## 4. Verification

- [x] 4.1 Format changed Go/frontend files and run focused backend/frontend tests.
- [x] 4.2 Run migration, OpenSpec, locale, docs, dependency, and release-metadata checks.
- [x] 4.3 Verify no conflict markers or active official Go module imports remain.
- [x] 4.4 Record residual risks; do not push, tag, publish a Release, or build/push images.

Residual verification note: the integration-tagged PostgreSQL rollup trigger,
DST, and rebuild tests and the full backend integration matrix passed against
an isolated PostgreSQL 18 database and Redis 8 on the Apple Container network.
Unit-tagged rollup/migration tests and the repository/service/frontend suites
also passed. The merge remains planned and unpublished; no tag, Release, or
image was created or pushed. WebSocket pooled handshake fingerprint carriers
retain their pre-existing behavior and are not covered by the HTTP header/body
convergence guarantee in this change.
