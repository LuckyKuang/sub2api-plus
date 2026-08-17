# Design: asynchronous image task management

## Ownership and status policy

Deletion uses the same authenticated `user_id + api_key_id` owner pair as task
listing, polling, and download. The history repository reads the requested task
only within that owner scope. A missing task and a task belonging to another
owner both produce the same not-found response. Only `failed` is deletable;
`processing` and `completed` produce a conflict and remain unchanged.

## Recoverable deletion order

The service first reads the owner-scoped PostgreSQL history status. After
validating `failed`, it deletes the exact Redis task key and then conditionally
deletes the PostgreSQL row using owner, task ID, and `status = 'failed'` in the
predicate. A missing Redis key is successful. If Redis fails, PostgreSQL remains
unchanged so the user can retry. The conditional database delete protects
against a concurrent terminal-state update and reports a conflict when the row
still exists in another status.

Object storage is deliberately not part of record deletion. Failed tasks do
not require an S3 scan, and task management must never infer keys from a prefix.

## Routing and middleware

Both `DELETE /v1/images/tasks/:task_id` and
`DELETE /images/tasks/:task_id` return `204 No Content` on success. Task
management classification covers exact list paths, item paths, downloads, and
deletion. Those paths bypass subscription, quota, and expiration enforcement;
API-key parsing, hard-disabled-key state, user-state, IP restrictions, and
handler ownership checks remain in force. List, detail, download, and deletion
remain available while new image generation is disabled as long as the task
stores are pollable.

## User interaction

Only failed rows show a destructive trash action. Activating it opens the
shared confirmation dialog with the task ID and an irreversible-action warning.
The clicked row alone shows pending state. Success closes matching task detail,
refreshes the current page, and moves to the previous page if deletion emptied a
non-first page. Failure keeps the row, page, and filters in place.

The API Key and status controls both use the shared Select component at their
existing responsive grid widths. This provides identical trigger height, text
alignment, chevron placement, truncation, and dropdown anchoring without
browser-specific native-select CSS.
