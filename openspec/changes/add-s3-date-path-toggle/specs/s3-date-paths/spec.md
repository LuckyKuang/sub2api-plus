## ADDED Requirements

### Requirement: S3 date directories must be independently configurable

The system SHALL store a stable base key prefix and an independent
`append_date_path` boolean for database backups and asynchronous image results.
When enabled, newly created object keys MUST insert `yyyy/MM/dd` after the base
prefix; when disabled, they MUST place the object name directly after the base
prefix.

#### Scenario: Backup date path is enabled

- **WHEN** the backup base prefix is `backups/`, the server date is 2026-08-17,
  and `append_date_path=true`
- **THEN** a new backup object key MUST start with `backups/2026/08/17/`

#### Scenario: Image date path is disabled

- **WHEN** the image base prefix is `images/` and `append_date_path=false`
- **THEN** a new image object key MUST start with `images/`
- **THEN** it MUST NOT insert a date directory before the task object name

### Requirement: Date paths must use the configured server timezone

The system SHALL resolve date paths when an object key is created using the
application's configured server timezone. It MUST NOT persist a concrete current
date in the base-prefix setting and MUST NOT use browser timezone for the date
boundary.

#### Scenario: Object creation crosses a UTC date boundary

- **WHEN** the configured server timezone still resolves an upload timestamp to
  2026-08-17 while UTC has reached 2026-08-18
- **THEN** the inserted date path MUST remain `2026/08/17`

#### Scenario: Split backup upload crosses midnight

- **WHEN** a backup record is created before the configured-timezone midnight
  and its parts upload after midnight
- **THEN** every part MUST remain under the date directory selected for that
  backup record

### Requirement: Existing object references must remain immutable

Changing a base prefix or date-path switch SHALL affect only newly created
objects. Restore, download, cleanup, and async-image ZIP operations MUST use the
exact object key recorded when each object was created.

#### Scenario: Async image is downloaded after midnight

- **WHEN** an image is uploaded before midnight and its ZIP is requested after
  midnight
- **THEN** the download MUST open the exact key stored at upload time
- **THEN** it MUST NOT reconstruct the key with the new current date

#### Scenario: Administrator changes the active image prefix

- **WHEN** a completed task stores image keys under `images-old/` and the active
  configuration changes to `images-new/`
- **THEN** that task's ZIP download MUST continue reading its stored
  `images-old/` keys

### Requirement: Defaults must preserve the current object layout

Missing or newly initialized backup settings SHALL enable date paths. Missing or
newly initialized async-image settings SHALL disable date paths.

#### Scenario: Existing backup JSON lacks the new field

- **WHEN** stored backup S3 JSON has no `append_date_path` member
- **THEN** the effective backup configuration MUST resolve it as true

#### Scenario: Existing image JSON lacks the new field

- **WHEN** stored async-image JSON has no `append_date_path` member
- **THEN** the effective image configuration MUST resolve it as false
