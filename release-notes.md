Sub2API Plus v0.1.177+custom.002

## Highlights

- Add independent S3-compatible date-path toggles for database backups and
  asynchronous image results.
- Add failed asynchronous-image task deletion and aligned API Key/status
  filters in the task history UI.
- Keep existing asynchronous-image task management available after a key is
  exhausted, expired, reassigned, or blocked from new image submissions.

## Changed

- Date-path settings retain a stable base prefix and resolve `yyyy/MM/dd` from
  the configured server timezone for each newly created object.
- Async-image ZIP downloads use private exact object keys, so a later prefix
  or date-path change cannot redirect a historical download.

## Fixed

- Revalidate OpenAI API-key and OAuth response/session affinity against current
  group membership and full account eligibility, including every turn of an
  already-established Responses WebSocket in all ingress modes. Follow-up
  turns now recheck billing, current model/capability/transport, and image
  permission before upstream writes, so policy changes take effect without
  waiting for sticky bindings or long-lived connections to expire.
- Keep Codex Responses and Alpha Search on one account route by recognizing the
  stable session in turn metadata; usage correlation and cyber-session blocking
  now use the same sanitized session value, and default hard-affinity priority
  changes no longer split an active conversation across accounts.
- Preserve configured image storage for already-accepted task completion and
  historical ZIP downloads when administrators disable only new submissions.
- Delete Redis task state atomically only while its current status is `failed`,
  preventing a concurrent completion from being removed.
- Keep all non-disabled owner keys visible for task-history management while
  restricting new submissions to currently eligible OpenAI/Grok keys.

## Compatibility and migration

- No database migration is required.
- Backup date paths default to enabled to retain the existing backup layout;
  async-image date paths default to disabled to retain the existing image
  layout. Changing either setting affects only future objects.
- Existing async-image task URLs remain usable. Tasks completed before this
  release lack private exact object keys, so their ZIP download may be
  unavailable until the normal task TTL expires.

## Known issues

- Async-image tasks completed before this release may not have exact stored
  object keys and therefore cannot be reconstructed safely for ZIP download.
  Existing direct result URLs remain unaffected, and the task records expire
  under their normal TTL.

## Upstream baseline

Official release: v0.1.177
Official commit: 073e92d17178a1ccdb0a27017f572f10c9c7ab62
