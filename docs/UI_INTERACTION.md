# Console layout and request inspection

The console retains its colors, typography, navigation and business permissions.
The brand, navigation, toolbar and business panels are independent solid cards.
User, admin, support and operations pages share the same fluid content boundaries;
there are no route-specific maximum page widths. Individual forms and external
content may retain a readable inner width.

`frontend/src/styles/console.css` owns the shared layout and surface tokens.

| Viewport | Outer inset on all four sides | Card gap | Navigation |
| --- | --- | --- | --- |
| Desktop, at least 1024px | 24px | 24px | 256px column; 72px when collapsed |
| Tablet, 768–1023px | 16px | 16px | Drawer |
| Phone, below 768px | 12px | 12px | Drawer |

Safe-area insets may increase the outer inset. The brand, toolbar and toolbar
inner row are 64px high. The toolbar centers its inner row across its border,
including fractional border widths at browser zoom; navigation and business
content start on the same horizontal line.
The toolbar and desktop sidebar stick at the outer top inset. Sticky settings
tabs and details derive their offsets from these tokens. `TablePageLayout`
measures the remaining viewport height, including the bottom outer inset, and
uses document scrolling below 1024px. Public/auth layouts retain their own geometry;
operations fullscreen remains a dedicated full-screen view.

Cards, nested sections, inputs, buttons, Select triggers, menus and dialogs use
solid theme fills with their existing edges, soft shadows, highlights and focus
states. Console surfaces use no backdrop filters or filter-only pseudo-elements.
The shared background image or gradient remains visible on the page canvas
between surfaces. Dropdowns must paint above subsequent cards, and fixed dialogs
must retain viewport positioning. Explicit scroll containers still own clipping;
searchable selects and shared dialogs retain their body portals.
Surface positioning is a low-specificity fallback and must preserve component
and utility positioning, including the sticky toolbar and absolute dropdowns.
Keep that fallback outside Tailwind's component layer so important card/dropdown
variants cannot promote positioning on unrelated surfaces.
Light/dark themes share geometry and solid surface behavior, including when
reduced transparency is requested. Reduced-motion preferences disable transitions.

On compact toolbars, secondary tools remain available in the user menu without
mounting duplicate locale/subscription controls. The mobile drawer traps focus,
locks background scrolling and makes the main content inert; Escape, backdrop,
close button and route navigation close it. It restores focus to the opener and
shares the dialog stack with other dialogs. Resizing to desktop closes the drawer.

Dialog keyboard loops include disclosure summaries. Collapsed disclosure content,
disabled controls, inert content and hidden surfaces cannot receive loop focus;
opening a disclosure makes its visible controls reachable.

## Usage lists

Plain selection controls, including request category and billing type filters,
use `Select.vue` and the same teleported panel as searchable model/type controls.
`searchable: false` hides the search input while preserving the shared menu,
option highlights, selected checkmark, keyboard navigation and light/dark theme.
The option count affects automatic search visibility only, never the menu renderer.
Trigger appearance is shared in `frontend/src/style.css`; panel appearance is
owned by `SearchableSelect.vue`. Menus teleport to the document body to escape
card clipping and stacking contexts. Searchable, clearable and composite controls
retain their integrated interactions. Compact controls may vary in size, with
translated labels clear of the arrow. Direct native selects are not used.

Historical usage restores the independent columns and cell content from
`9c05b0c37ff05abc375b0a35fd8cffc1727837b0`. User order is API key, model,
reasoning effort, endpoint, IP, group, type, billing mode, tokens, cost, latency,
time, User-Agent (only User-Agent starts hidden). Admin defaults are user,
account, model, reasoning effort, service tier, endpoint, group, type, tokens,
cost, latency, time and User-Agent. API key, billing mode, request ID, session ID
and IP start hidden and remain available through column settings. Saved personal
column settings take precedence; reset restores these defaults. Wide tables
scroll within their card. Existing precision, permissions, billing and timing
definitions remain.

Usage and live rows have no side panel, row detail selection, or detail request.
Old `record` query links return to the list while retaining other queries and
hash. Established usage detail APIs remain available to other clients. Admin
error records retain the established diagnostic modal. Shared detail workspaces
on other pages retain their existing responsive behavior.

Live columns are time, admin-only user/key, model, type, group, result, speed,
performance, input → output, billed cost, client/IP. **Performance is visible by
default**; only client/IP starts hidden. Time, model and result are fixed. Column
menus retain integrated checkboxes. Layout-version preference keys isolate role,
authenticated identity and support target; the restored layouts do not inherit
compact-era hidden columns, and do not delete old preferences. Reset restores
the stated defaults.

Result uses a colored dot and short label/reason. Processing and observed
transmission are active; completed is successful; explicit failures, rejections,
cancellations, disconnects and incomplete responses are abnormal. Handler-returned
`finished` is status pending; expired observations are unknown. Pending and unknown
belong only to All, never the successful or abnormal filters. HTTP status is
auxiliary metadata. Reasons use stable public categories, never raw diagnostics.
Only the existing semantic first-output observer advances transmission; headers,
keepalives and lifecycle frames do not. No response parser is added.

Speed shows measured first byte and forwarding duration; active elapsed is labeled
separately and never substitutes for forwarding duration or the TPS denominator.
Performance uses strict first-token timing and the existing request-average TPS.
Real timing version, native compaction and media counts govern availability. There
is no independent reasoning-token count in usage, so none is displayed. TPS has
no formula/native title tooltip; one latency popup retains measured timings and
unavailability reasons, accessible on hover, focus and touch.

Phone rows show model/type and cost, result/time, then tokens/cache and group.
More fields expands within the same stable row, with full group, performance,
client/IP and copy access, using only already-received data. No drawer or detail
fetch is involved. Long model names, IPv6 and User-Agent wrap inside the row.
Only the current desktop/mobile layout is mounted.

Live result/model/group and admin user filters operate on the received window,
not all historical requests. Export current window exports only those filtered
rows, preserving exact numeric values and blanks for missing fields. CSV text
cells are escaped against spreadsheet formula injection. Full-history export
remains in history.

## Historical insertion boundary

`GET /usage` and `/admin/usage` accept optional `snapshot_max_id`: omit for existing
behavior, `new` to capture the maximum matching ID under the authenticated filters,
or a nonnegative integer to read only IDs at/below that boundary. Requested
boundaries are returned in `snapshot_max_id` and apply to list and count queries.
Sorting retains an ID tie breaker.

The UI holds the boundary for paging/export, resetting on filters or explicit
refresh. Polling reads a fresh boundary separately; reading old pages
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
| History | On, 30s; optional 15/30/60s | List only |
| Live | On, 2s; at least 5s when empty | Redis observation snapshot + bounded canonical usage lookup |
| Overview | 30s | Statistics; charts at least 60s apart unless explicitly requested |

Visible live tabs of the same identity divide an approximate 30-reads/minute
budget using expiring local-storage registrations. Hidden tabs unregister;
restricted storage uses a conservative interval. This is best effort, not a quota
guarantee. Existing global/Heavy limits remain authoritative and are not raised.

Usage pages expose a switch, actual next-refresh countdown and manual refresh.
Only the interval persists. Off lasts for the current stay: browser reload,
route/tab reactivation and browser foreground return enable and fetch once.
Manual refresh while off reads once and remains off; a mere network reconnect
respects off. Hidden/offline periods pause scheduling, with server Retry-After
and budget/backoff still authoritative. Other pages retain their prior policy.
Existing visible rows update while reading; insertions wait behind a new-record
notice while scrolling, selecting text, focusing controls or expanding mobile
rows. Background reads retain row identity and expansion.

HTML revalidation includes the per-response CSP nonce in its ETag. Reloading
must return a fresh HTML body when the nonce changes, so the cached initialization
script always agrees with the response policy. Fingerprinted asset caching is unchanged.

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
`finished`, not proof of successful protocol completion or billing. Existing
stream errors override HTTP 200; known WS terminal outcomes stay independent per
logical turn. No success rate, ETA, completion percentage or token estimate is
inferred.

The existing usage builders attach the final billing key (request ID, user and
API key) to the specific HTTP observation or WS turn through task context. This
does not change billing-key precedence or deduplication. Associations expire in
Redis; ambiguous multiple money events are not assigned a session fee. The read
service performs one bounded lookup (at most 200 keys) of persisted usage, with
batch group names. There are no browser per-row reads or statistics scans. Actual
cost, cache/media counts, inbound User-Agent/IP, timing and completion come from
those existing records. Missing records remain unlinked, never free. Partial
usage and tiny nonzero fees retain their actual values. User projection never
exposes account routing or admin cost/diagnostics; the existing user-error setting
also hides observed rejections and failures. Explicit stream failures cannot be
overwritten by a completed usage row.

One 250ms display clock uses server time and monotonic elapsed time. Interpolation
stops on pause, error or a 15s data gap, retaining the last displayed elapsed value.
Final measured forwarding duration remains separate. A retained active record
absent from a later snapshot becomes unknown. Support target/auth changes discard
old responses and remount the corresponding page.

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
## Global console appearance

In **System Settings → General → Appearance and background**, administrators can
upload or replace a shared background and adjust light/dark canvas masks
independently. The setting applies to both admin and user consoles. Each viewer
retains their own theme choice. The background and masks affect the page canvas;
the sidebar, header, content cards, controls, tables and popup panels keep solid
theme fills. There is no card-transparency control or client binding that changes
surface opacity. Ancestor `opacity` and container filters must not be used to
implement appearance settings.

Upload and adjustment produce a local draft. Page preview affects only that
administrator and ends when leaving General or the settings page. **Save
appearance** publishes this card independently from other general settings.
**Restore defaults** is also a draft until saved: it clears the image, restores
the existing gradient, and resets light/dark masks to 30%/48%. Preview content
surfaces remain solid `#f8faf9` in the light theme and `#262c2d` in the dark theme.
Uploading, previewing, saving or resetting the background must not introduce a
card-transparency default or change the stored compatibility value described
below.

JPG/PNG/WebP input is limited to 10 MiB and 40 megapixels. The browser flattens
and compresses it to JPEG, at most 2560 pixels on its longest side and 2 MiB.
The server validates bytes, declared format, full decode, dimensions (at most
4096 per side and 16 megapixels), and re-encodes JPEG to strip metadata and
animation before atomically storing the image and public metadata in the
existing settings table. No extra filesystem volume or environment variable is
required. Do not commit user images. A failed validation or database write must
not replace the active appearance.

`GET/PUT /api/v1/admin/settings/appearance` use the existing admin authentication,
rate limiting, compliance guard, and operation audit. PUT accepts `light_mask`,
`dark_mask`, nullable `card_transparency`, and
optional `background_image` (JPEG/PNG/WebP data URL; omitted/null keeps the
current image, empty string removes it). Percentages must be integers in
0–100. Responses, `GET /api/v1/settings/public`, and injected `__APP_CONFIG__`
expose `console_appearance` metadata only: `background_url`, the two masks,
and transparency. The `card_transparency` API/storage field retains its existing
protocol and validation for compatibility, without a migration. The appearance
editor preserves and round-trips its current value; console rendering ignores
it. Image bytes are never embedded in these public payloads.

`GET /api/v1/settings/console-background/:revision` publicly serves the active
JPEG with a SHA-256 URL, ETag, `nosniff`, and immutable caching. A replaced or
cleared uncached URL returns 404 with `no-store`; already cached images may
remain in browser caches until expiry. Saving invalidates the HTML settings
cache and immediately updates the administrator's public-settings cache. Other
viewers get the published appearance on refresh. Failed image loads fall back
to the built-in gradient, and stale load completions must not replace a newer
image or a reset. Table bodies, sticky headers and fixed columns use solid theme
surfaces with no backdrop filters. Pinned cells cover horizontally scrolled
content, with hover, selection, edge shadows, stacking and focus intact. Long
document-scrolling usage lists must remain fully painted after repeated vertical
jumps. This solid-surface rule applies across console pages, including outer
cards and popup panels.

Console surfaces use the shared tokens in `frontend/src/styles/console.css` and
`surfaces.css`. `surface-card` paints standalone cards, `surface-inset` paints
nested sections, `surface-control` paints buttons, and `surface-panel` owns
dropdown/custom-dialog paint. `surface-overlay` is a separate, lightly
dimmed full-screen scrim without a backdrop filter. Shared Select, date-range,
proxy, dialog and button styles use these same tokens. New controls must retain
their integrated search, checkbox, focus, error and disabled behavior.

The main solid surface is `#f8faf9` in the light theme and `#262c2d` in the dark
theme. Inset sections use `#edf2f0` and `#202627`, respectively. Shared card,
control, panel and table tokens do not depend on card transparency or backdrop
filter support. Deep-theme secondary text uses `#c1cdd1`, with distinct primary,
selected and danger colors. Do not dim ancestor opacity or filter containers
holding fixed menus. Preserve opaque switch thumbs, status/progress marks and
QR quiet zones (`surface-opaque`).

The date-range menu retains its trigger alignment when it fits, and otherwise
stays at least 12px from the horizontal viewport edges. Opening, scrolling or
resizing the menu must not apply date changes; Apply retains that responsibility.
