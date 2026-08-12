Sub2API Plus v0.1.173+custom.003

## Highlights

- Restore Codex CLI/TUI session IDs in admin usage details and Excel exports.

## Fixed

- Accept the native `session-id` request header sent by Codex CLI/TUI 0.147.0
  when persisting usage records, while preserving all existing compatible
  session-header aliases.
- Keep `thread-id` separate from the persisted usage session ID so thread and
  session identity retain distinct meanings.

## Compatibility and migration

- No database migration or configuration change is required.
- Only usage records created after deploying this release can contain the
  newly recognized Codex session ID; historical NULL values are unchanged.

## Known issues

- Historical usage records with an empty session ID cannot be reliably
  backfilled.

## Upstream baseline

Official release: v0.1.173
Official commit: 29009f0b2ea14edf3b11ae2564fb617ff91a03b4
