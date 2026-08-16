## Why

Official Sub2API v0.1.177 adds server-timezone group-usage rollups, native
Codex remote compaction v2, turn-state relay, and Grok billing corrections.
Those changes overlap Plus-owned release, OAuth session-policy, outbound
identity, fingerprint convergence, quota, and observability paths. A
mechanical merge would change security and identity behavior.

## What Changes

- Merge official tag `v0.1.177` commit
  `073e92d17178a1ccdb0a27017f572f10c9c7ab62` and no later upstream commit.
- Adopt official migrations 222 and 223 and the complete daily-rollup
  lifecycle, using the server-configured application timezone.
- Adopt native remote compaction v2 while retaining a separate legacy
  `/responses/compact` path and Plus path-validation and profit controls.
- Relay `x-codex-turn-state` with credential-owner provenance and commit-time
  recording so failed attempts cannot authorize later echoes.
- Preserve Plus OAuth session access, local quota headers, usage completeness,
  client identity source precedence, and distribution metadata.
- Keep the Codex fingerprint convergence default at Plus `session`: missing,
  empty, or invalid values resolve to `session`; only explicit `off` disables
  convergence for OAuth accounts.
- Prepare `v0.1.177+custom.001` metadata with a planned, unpublished upstream
  mapping. This change does not create or push a tag, Release, or image.

## Impact

- Persistent data: migrations 222 and 223 create and timezone-key group-usage
  daily rollups. A configured timezone change rebuilds the derived buckets.
- Public API/UI: group usage no longer accepts browser timezone input and adds
  yesterday cost.
- Security boundary: turn-state echo is limited to the credential owner that
  received it; OAuth session authorization still precedes outbound mutation.
- Protocol routing: native compaction remains on `/responses`; legacy compact
  remains on `/responses/compact`.
- Operations: the embedded version and `UPSTREAM.md` advance to the official
  v0.1.177 baseline without publication.
