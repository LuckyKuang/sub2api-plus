# Add configurable S3 date paths

## Problem

Database backups always add a `yyyy/MM/dd` directory after the configured S3
prefix, while asynchronous image results never do. Administrators cannot choose
the object layout independently for the two workloads, and the current async
image ZIP download reconstructs keys from the active prefix, which is unsafe
once a date path or a new prefix can take effect.

## Proposal

- Add an `append_date_path` setting beside each backup and async-image key prefix.
- Keep the stored prefix as a stable base and resolve the concrete date for each
  newly created object using the configured server timezone.
- Preserve current defaults: enabled for backups and disabled for async images.
- Store exact async-image object keys in private task state and use those keys for
  ZIP downloads instead of reconstructing them from current configuration.
- Show the effective key shape in the admin form and keep English and Chinese
  labels aligned.

## Non-goals

- Moving or renaming existing S3 objects.
- Making the date format configurable.
- Applying browser or per-user timezones to object paths.

## Impact

- Persistent settings: backup and async-image S3 JSON gain
  `append_date_path`.
- Runtime configuration: `image_storage.append_date_path` and its environment
  binding are documented.
- Internal async task state gains private exact object keys.
- Admin API types, UI, locales, tests, and object-storage documentation change.
