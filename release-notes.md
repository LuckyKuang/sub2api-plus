Sub2API Plus v0.1.177+custom.001

## Highlights

- Merge official Sub2API v0.1.177 as the new Plus baseline.
- Add server-timezone daily group-usage rollups and yesterday cost in the
  administration UI.
- Support native Codex remote compaction v2 on `/responses` while retaining
  the separate legacy `/responses/compact` path.
- Relay `x-codex-turn-state` with credential-owner provenance and shared-cache
  protection against known cross-account echoes.

## Changed

- Use `TZ`, then `TIMEZONE`, then the configured/default application timezone
  for persistent group-usage day boundaries. Browser timezone is no longer
  sent by the group usage page.
- Advertise the session-level `remote_compaction_v2` Codex beta feature on
  OAuth HTTP and WebSocket traffic, and require a compaction output item from
  the native account probe.
- Restore the account-page auto-refresh preference during module
  initialization and keep the frontend nanoid lock entry aligned at 3.3.18.

## Fixed

- Apply Grok long-context pricing from the group switch without an OpenAI
  account-level veto.
- Keep unknown Grok image, video, and audio model families out of the text
  fallback price card.
- Invalidate and rebuild group rollups after recompute, retention cleanup,
  partition deletion, late historical writes, or configured timezone change.

## Compatibility and migration

- Migration `222_group_usage_daily_rollups.sql` creates derived daily group
  usage buckets and synchronization state.
- Migration `223_group_usage_rollup_timezone.sql` records the bucket timezone
  and rebuilds derived data when that timezone changes.
- Codex OAuth fingerprint convergence keeps the Plus default: missing, empty,
  or invalid `codex_fingerprint_mode` values use `session`; only explicit
  `off` disables convergence. Create, edit, and bulk forms use the same rule.
- Codex outbound identity precedence remains credential-owner account user
  agent, then global user agent, then the compiled default.

## Known issues

- Turn-state provenance uses Redis when the configured GatewayCache supports
  it and falls back to process-local best effort if the shared cache is
  unavailable.
- Daily rollups are derived data; the first startup after migration or a
  timezone change may perform additional synchronization work.
- Client-profile and fingerprint controls cannot attest a physical client
  binary or device.
- HTTP fingerprint convergence keeps headers and map/raw `client_metadata`
  coherent. Pooled WebSocket handshake fingerprint carriers retain their
  pre-existing behavior and are not converged with each request payload by
  this release.

## Upstream baseline

Official release: v0.1.177
Official commit: 073e92d17178a1ccdb0a27017f572f10c9c7ab62
