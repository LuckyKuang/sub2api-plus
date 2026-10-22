# Console layout and request inspection

The console retains its colors, typography, navigation and business permissions.
The brand, navigation, toolbar and business panels are independent glass cards.
User, admin, support and operations pages share the same fluid content boundaries;
there are no route-specific maximum page widths. Individual forms and external
content may retain a readable inner width.

`frontend/src/styles/console.css` owns the shared layout and surface tokens.

| Viewport | Outer inset on all four sides | Card gap | Navigation |
| --- | --- | --- | --- |
| Desktop, at least 1024px | 24px | 24px | 256px column; 72px when collapsed |
| Tablet, 768–1023px | 16px | 16px | Drawer |
| Phone, below 768px | 12px | 12px | Drawer |

Safe-area insets may increase the outer inset. The brand and toolbar are 64px
high; navigation and business content start on the same horizontal line.
The toolbar and desktop sidebar stick at the outer top inset. Sticky settings
tabs and details derive their offsets from these tokens. `TablePageLayout`
measures the remaining viewport height, including the bottom outer inset, and
uses document scrolling below 1024px. Public/auth layouts retain their own geometry;
operations fullscreen remains a dedicated full-screen view.

Cards use translucent fills, a fine edge, soft shadow and backdrop blur over a
subtle shared gradient. Dense tables, inputs and fixed columns use more opaque
fills for legibility. Nested panels and rows do not add another blur layer.
Light/dark themes share geometry. Unsupported blur and reduced-transparency
preferences use an opaque surface; reduced-motion preferences disable transitions.

On compact toolbars, secondary tools remain available in the user menu without
mounting duplicate locale/subscription controls. The mobile drawer traps focus,
locks background scrolling and makes the main content inert; Escape, backdrop,
close button and route navigation close it. It restores focus to the opener and
shares the dialog stack with other dialogs. Resizing to desktop closes the drawer.

## Lists and details

User usage defaults to seven columns: time, model/type, key/group, completion,
tokens, billed cost and performance. Admin usage has eight: time, user/key,
account/group, model, completion, tokens, cost and performance. Detailed columns
remain in the integrated column menu and exports. Preferences are scoped by
authenticated identity and support target; legacy hidden-column arrays migrate
once per scope. The reset action restores recommended columns.

Error lists default to six user / seven admin columns, retaining key/group
summaries, local scrolling for optional columns and a keyboard detail entry.
Admin error selection highlights the active row. The separate operations table
retains its own column configuration.

Details start closed. At **1520px of actual workspace width**, a 440px panel and
24px gap fit beside the summary table and its panel padding. The threshold uses
the available content width after navigation and outer insets. Smaller workspaces
use a right overlay, maximum 480px;
phones use the viewport. Extra columns can scroll inside the table. The sidebar
is never automatically collapsed.

Usage details use `?record=<id>`, preserving other query parameters. First selection
pushes history; subsequent selections replace it. Close returns to the list;
closing a direct link removes only `record`. Closing after filters change retains
the current query and hash instead of restoring the old list location. Previous/next stays within the loaded
page. Late responses cannot replace a newer/closed selection. Missing or forbidden
records display a local failure. Error/live selections use the same responsive
workspace but are transient, without persistent deep links.

Overlays reuse the dialog stack, focus loop and scroll lock. Docked details do
not lock the list. Escape closes details and focus returns to the opener when
still mounted. Row buttons, links and input controls do not also open details.
Each usage row has a keyboard detail button.

User detail uses owner-checked `/usage/:id`. Its established public numeric
`account_id` remains API-compatible; the user UI does not show admin account
details. `/admin/usage/:id` uses the admin guard/projection, including routing and
account costs. Support reads retain the target-user projection.

## Historical insertion boundary

`GET /usage` and `/admin/usage` accept optional `snapshot_max_id`: omit for existing
behavior, `new` to capture the maximum matching ID under the authenticated filters,
or a nonnegative integer to read only IDs at/below that boundary. Requested
boundaries are returned in `snapshot_max_id` and apply to list and count queries.
Sorting retains an ID tie breaker.

The UI holds the boundary for paging/export, resetting on filters or explicit
refresh. Polling reads a fresh boundary separately; reading old pages/details
shows an update notice instead of inserting rows. Exports retain initial filters.
No exact new-record count is inferred from offset pagination.

This is an insertion boundary, **not an MVCC snapshot**. Deletions, reconciliation
and transactions that reserved an older ID before committing can still change
an offset page. No long-lived DB transaction is kept for a browser. Query afresh
after cleanup.

## Refresh and motion

Initial loads use skeletons; background refresh preserves rows/charts.
`useAutoRefresh` coalesces pending work, pauses while hidden/offline, resumes once,
honors seconds/date `Retry-After`, and uses jittered exponential backoff capped
at 300 seconds. Callers must return the actual pending promise. Poll errors keep
old data and an inline stale indicator, without repeated error toasts.

| View | Default | Reads |
| --- | --- | --- |
| History | Off; optional 15/30/60s | List only |
| Live | 2s; at least 5s when empty | Redis observation snapshot |
| Overview | 30s | Statistics; charts at least 60s apart unless explicitly requested |

Visible live tabs of the same identity divide an approximate 30-reads/minute
budget using expiring local-storage registrations. Hidden tabs unregister;
restricted storage uses a conservative interval. This is best effort, not a quota
guarantee. Existing global/Heavy limits remain authoritative and are not raised.

Refresh icons spin only during I/O. Right overlays use a 200ms transform; figures
use tabular digits. Overview charts disable replay animation.
`prefers-reduced-motion` disables console transitions and animations.

## Live observation API and limitations

`GET /usage/live`, `/admin/usage/live` and the support read route return `items`,
`server_now`, `window_seconds` (300), `limit` (200), `truncated` and
`poll_after_seconds`, with `Cache-Control: no-store`. Unavailable Redis returns
503 instead of an invented empty success.

The gateway attaches local metadata first and publishes externally only after
the existing security audit returns. Observation does not change audit, identity,
admission, scheduling, retry or billing decisions. Writes are best effort, bounded
to 32 concurrent writes and 200ms each. Failures emit sampled structured diagnostics.
Values are snapshotted before asynchronous publication; monotonically increasing
versions stop older heartbeats from replacing terminal states. Bodies and
credentials are never stored.

Records expire after 360s without renewal. Indexes retain at most 2000 global / 200
per user; global eviction also removes payloads. Reads return at most 200 recent
observations. `truncated` conservatively reports reaching the limit. Capacity loss
can occur. Unrenewed processing observations become unknown after 45s, never failed.

Each HTTP request and audited Responses WS turn receives a server UUID. Failover
attempts keep the logical turn ID even if the proxy restarts turn numbering. Live
sideband is one session. Async endpoints observe submission, not background task
execution. Unauthenticated/basic-validation rejections are outside this stream;
existing task/error history covers other records.

`processing` indicates no finer stage was observed. HTTP handler return is
`finished`, not proof of successful protocol completion or billing. HTTP/stream
errors, cancellations and known WS completion/partial outcomes are distinguished.
WS final timing/counts can use existing average TPS; missing HTTP timing/counts
remain unavailable. No token estimate, ETA, percentage, success rate or billing
linkage is invented. Historical usage/error records remain authoritative.

One 250ms display clock uses server time and monotonic elapsed time. Interpolation
stops on pause, error or a 15s data gap, retaining the last displayed elapsed value.
Final observed elapsed values replace interpolation. A selected processing record absent from
a later snapshot becomes unknown. Owner-scoped user reads omit account routing;
the user-error setting also hides observed failures. Support target/auth changes
invalidate old responses and remount the corresponding page.

## Overview definitions

Today usage records count persisted rows including partial/aborted rows, not all
admitted requests. Billed cost uses `actual_cost`; tiny nonzero amounts remain
visible. Available balance must not subtract frozen balance again. Quotas remain
separate. RPM/TPM are five-minute averages; user TPM excludes cache. Mean forwarding
duration is cumulative, not today's mean.

User dashboard queries honor browser `timezone`, falling back to server settings
for absent/invalid zones. `generated_at` is query start, not a consistent snapshot
across separate profile/quota/statistics/chart queries. Selected chart dates are
independent of today cards. [USAGE_TIMING](USAGE_TIMING.md) formulas remain unchanged.
