# Design: administrator user support view

## Identity and mode boundary

The authenticated administrator is always the actor. The account selected in
the sidebar is only a target. The UI is in self mode when no target is selected
or when the target ID equals the actor ID. Self mode continues to use the
existing `/keys`, `/async-image`, and other personal routes with all current
operations. Any different target ID, including another administrator, uses the
read-only support namespace.

Neither frontend state nor backend middleware replaces the authenticated user.
Direct navigation to a support route whose target equals the actor redirects to
the matching personal route. Leaving the support namespace does not make normal
administrator management pages read-only.

## Explicit support routes

Support pages use `/admin/support/users/:user_id/...` and backend APIs use
`/api/v1/admin/support/users/:user_id/...`. The backend registers only GET
routes. Dedicated query handlers receive the target ID explicitly and do not
reuse mutation-capable user handlers or API-key-authenticated gateway calls.

Initial support resources are overview, API-key summaries, asynchronous-image
history, usage, channels, channel status, subscriptions, orders, and basic
profile data. Purchase, redemption, affiliate transfer, batch-image, and
security settings are absent from support navigation.

## API-key confidentiality

The ordinary self endpoint `/api/v1/keys` remains unchanged and may return the
authenticated owner's key because existing owner CRUD depends on it. Every
administrator endpoint that lists a target user's keys returns a separate
summary DTO that has no plaintext-key field. The legacy
`/api/v1/admin/users/:id/api-keys` endpoint adopts the same safe DTO so the new
support boundary cannot be bypassed through an older administrator path.

The summary may include key ID, display name, status, group, quota, usage,
rate-limit windows, expiration, timestamps, concurrency, and counts indicating
whether IP restrictions exist. It must not include key material, authorization
headers, full allow/deny lists, export payloads, or client-import data.

## Asynchronous-image support queries

The current user page loads API-key plaintext and calls gateway routes scoped
to the exact `(user_id, api_key_id)` owner pair. That behavior remains unchanged
in self mode. Support mode instead queries durable `async_image_tasks` history
by target user ID. It may optionally filter on an API-key ID that belongs to the
same target, but never loads the key value.

Support list and detail operations are repository reads only. They do not call
the Redis mutation path, submission, deletion, archive download, retry, status
repair, or user gateway handlers. Ordinary gateway list/detail/delete/download
ownership remains scoped to the exact user and API key.

## Frontend separation

The route parameter is the source of truth for a non-self support target. A
small store caches selector results but revalidates target display metadata on
each support page load and never overwrites the authentication store. The
sidebar maps self navigation to the existing personal routes and non-self
navigation to support routes. Route IDs are accepted only when they are
positive safe integers; invalid IDs return to the corresponding personal page.

Each page request captures a monotonically increasing sequence, target ID, and
resource before starting network work. Only the latest matching request may
write target or resource state, so a slower response from a previously selected
account cannot appear under the current target banner.

Support pages may reuse presentation-only tables, badges, pagination, charts,
and preview components. They do not mount mutation dialogs or import key-copy,
client-export, image-submission, task-deletion, or archive-download functions.
A persistent banner identifies the target and the read-only boundary.

## Audit and failure behavior

Sensitive support GETs record the authenticated administrator as actor and the
selected user as target. A disabled but non-deleted user remains viewable. A
missing or soft-deleted target returns not found; the frontend clears the
selection and returns to self mode. A non-administrator receives forbidden.

Audit recording is allowed because it does not mutate the target account or its
resources. Support reads must not touch last-active timestamps, mark content as
read, reset counters, refresh credentials, or change task state.

All validated support responses send `Cache-Control: no-store`. Usage summaries
query the ordinary usage statistics service with the explicit target ID and a
requested-model filter; they expose summary totals only. Today, week, and month
use the browser timezone automatically supplied by the API client, with week
starting Monday and month starting on day one.
