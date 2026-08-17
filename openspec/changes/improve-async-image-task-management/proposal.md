# Improve asynchronous image task management

## Problem

Failed asynchronous image tasks remain in the user-visible history with no way
for their owning API key to remove them. The API Key and status filters above
that history also use native browser selects, which render with slightly
different internal spacing and chevrons across platforms.

## Proposal

- Add equivalent authenticated deletion endpoints for versioned and
  non-versioned asynchronous image task paths.
- Permit deletion only for failed tasks owned by the authenticated user and API
  key, without revealing tasks owned by another key.
- Remove the task's Redis execution state and PostgreSQL history row while
  leaving object storage untouched.
- Keep task list, detail, download, and deletion requests outside billing
  enforcement while retaining all authentication and ownership checks.
- Add a failed-row delete confirmation to the user task list and align both
  filters by using the shared Select component.

## Non-goals

- Deleting processing or completed task history.
- Bulk task deletion.
- Scanning or deleting asynchronous image objects from S3-compatible storage.
- Changing task submission, execution, expiration, or billing behavior.

## Impact

- Public gateway API: two equivalent `DELETE` task endpoints are added.
- Persistent data: failed rows may be removed from `async_image_tasks`; no
  schema migration is required.
- Redis: the matching `image_task:{task_id}` key is deleted when present.
- Gateway middleware, backend services and repositories, frontend API and UI,
  locales, tests, and async-image documentation change.
