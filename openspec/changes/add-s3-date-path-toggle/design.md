# Design: configurable S3 date paths

## Configuration semantics

Both object-storage forms persist a base prefix and an independent
`append_date_path` boolean. The base prefix never contains a resolved current
date. A missing backup boolean resolves to `true` because backups already add
date directories; a missing async-image boolean resolves to `false` because
images currently use only their base prefix.

The effective object key is:

```text
<normalized-base-prefix>/[yyyy/MM/dd/]<object-name>
```

The date segment is present only when the corresponding switch is enabled.
Trailing slashes are normalized without changing other prefix characters.

## Time boundary

Object creation captures one application-timezone timestamp and uses it for the
entire key. No timer mutates stored configuration at midnight. The existing
global server timezone is the only date-boundary authority; browser timezone is
not involved.

For a split database backup, every part uses the directory selected when the
backup record was created, even if splitting or upload crosses midnight.

## Async image read integrity

The uploader returns the exact key used for each successful image upload. The
task service stores those keys only in the private task record; public task
responses continue to expose URLs, not object keys. ZIP download reads the exact
stored key and therefore remains correct after midnight or after the active base
prefix/date-path setting changes.

## Existing objects

No object is moved. Backup records already carry exact single-file and part
keys. Async task keys are captured for newly completed tasks. Changing either
switch affects only objects created after the updated setting becomes active.

## UI

Each Key Prefix field has an adjacent binary switch labeled "Append date path"
and a compact key-shape preview. The preview uses the literal `yyyy/MM/dd`
template so it cannot disagree with the server timezone.
