## 1. Planning and merge

- [x] 1.1 Record v0.1.170 baseline, release identity, compatibility scope, and outbound identity invariants.
- [x] 1.2 Merge official v0.1.170 with a normal three-way merge and resolve all conflicts without dropping Plus behavior.
- [x] 1.3 Regenerate Wire output after resolving service constructor changes.

## 2. Official v0.1.170 behavior

- [x] 2.1 Preserve interrupted-stream usage accounting, streamed 429/capacity retry, and bounded worker-pool shutdown fallback.
- [x] 2.2 Preserve Codex namespace, passthrough instructions, encrypted compaction recovery, and tool-output media fixes.
- [x] 2.3 Preserve moderation proxy, SMTP, image, Grok, payment, subscription, admin, pricing, and compact-home changes.

## 3. Codex outbound identity

- [x] 3.1 Resolve identity from credential owner, global setting, then compiled default and centralize shadow-parent behavior.
- [x] 3.2 Thread resolved identity through OAuth refresh/enrichment, PAT validation, and Agent Identity retry.
- [x] 3.3 Bind existing-account reauthorization context in server-side OAuth sessions and preserve user-agent on credential replacement.
- [x] 3.4 Align OAuth models query/header version and add account User-Agent validation and UI.
- [x] 3.5 Add source-level protection against the legacy `codex-cli/0.91.0` identity literal.

## 4. Verification and release preparation

- [ ] 4.1 Add focused integration tests for account/global/default identity across HTTP, WebSocket, images, OAuth, PAT, models, quota, and shadow accounts.
- [x] 4.2 Run backend, frontend, OpenSpec, release, deployment, and Apple Containers checks required by the changed paths.
- [ ] 4.3 Synchronize version, Docker arguments, upstream mapping, generated release docs, and release notes for v0.1.170+custom.001.
