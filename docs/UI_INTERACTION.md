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
The desktop console fills the viewport. Its toolbar and brand start at the same
outer top inset; the toolbar stays in the shell's normal flow without applying
the sticky inset a second time. The content below it scrolls independently.
Workspace pages keep their title, navigation, filters and pagination fixed and
allocate the remaining height to their content or table body. `AppLayout`'s
`workspace` prop and `styles/workspace.css` own this layout. `TablePageLayout`
uses the same available space for its table. Below 1024px, pages return to normal
document scrolling. Public/auth layouts retain their own geometry, and operations
fullscreen remains a dedicated full-screen view.

Dashboard quick actions are removed. Desktop dashboards fit their available
height; shorter administrator windows switch between the model/trend charts and
user trend instead of hiding charts below the viewport. The user dashboard
separates charts, platform/quota information and recent usage. Date controls and
refresh remain available; chart dates do not change the semantics of today's KPI
cards.
On desktop windows shorter than 800px, KPI cards share a horizontally scrolling
row so the charts retain usable height. All KPI cards remain available; phone
layouts keep their natural vertical flow.

Operations separates monitoring, alert events and system logs; risk control
separates configuration and audit records; IP access control separates automatic
protection, trusted proxies, login failures and rules. Existing rule links activate
the rules tab. Admin and user usage pages separate chart statistics and usage
lists while retaining their existing live/history/error/ranking subviews and
shared date selection. Switching presentation tabs does not reset filters,
pagination or drafts. Channel-monitor mode switching retains its original
component lifecycle so inactive modes do not acquire background polling.
Fixed filter areas use the available width before wrapping. In shorter usage
workspaces, the filter fields share a horizontal row with actions below it;
search results stay portalled outside that row so scrolling cannot clip them.

Plugin management and channel monitoring keep their heading/control cards fixed.
Prompt Audit keeps its title, events/configuration navigation, event filters and
pagination fixed, scrolling only event rows; configuration scrolls independently.
System Settings keeps its navigation and save actions in the same form with an
independently scrolling configuration body, preserving hidden sections and drafts.
My Orders keeps its search and filter card and pagination fixed while order rows
scroll. Admin Order Management and Subscription Plans use the same workspace:
query controls, refresh/create actions and pagination stay outside the scrolling
table. Available Channels fixes its title, description, filters and result count,
scrolling only the channel-card area and its loading, error or empty state.
In desktop windows up to 600px high, platform filters share a horizontally
scrolling row so channel cards retain visible space.
The purchase catalog keeps the subscription/recharge navigation visible,
uses four columns for desktop plans and preset recharge amounts, and keeps checkout
actions visible. On desktop, navigation and checkout actions occupy separate fixed
layout slots around the catalog's scrolling body. The channel-status monitoring
toolbar likewise sits above its own scrolling activity area. Content cannot pass
behind these controls, including at 100% transparency. Mobile layouts retain
natural page scrolling without transparent sticky cards overlapping content.

Scroll clipping belongs to the actual viewport: large content viewports use the
outer card radius, and inset table viewports use the panel radius. Their corners
remain rounded at the start, middle and end of scrolling. Table headers and frozen
columns keep their column alignment. Desktop scrolling tables place the visible
header above the body scrollport, so even rapid upward scrolling cannot paint
records behind a transparent header. Portalled menus and dialogs retain their
positioning; no global mask, transform or clip-path is used.

Main content shells, full-width headings, filters, navigation and pagination have
no outline, inset edge or ring, including their header/footer seams. Standalone
settings sections and composite workspaces follow the same rule. Nested business
cards, grid cards, controls and record rows retain their own borders. This keeps
partially filled grids and nested cards from exposing an extra frame.
Secondary navigation uses rounded 1px control outlines and the shared accent for
hover and selection; it has no underline or full-width bottom baseline.

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

## Rounded surfaces and hover cards

Shape belongs to the shared surface system, including transient UI. Shared radius
defaults are `--surface-radius` (24px desktop, 20px below 1024px) for outer cards,
16px for inset sections and popup panels, 12px for controls and 10px for menu items.
Different control sizes and existing local styles may retain other rounded radii.
Menus have inner padding so hover, focus and selection backgrounds remain
independent rounded regions.
Searchable select menus fit the available viewport height, keeping their search
field visible while options scroll.
Pagination uses separate rounded buttons rather than connected square segments.
Modal/card headers inherit the parent's top corners and footers inherit its
bottom corners; changing the background must never repaint square outer corners.
Do not add global overflow clipping: inline overlays must still escape cards,
and explicit table/body scroll containers keep ownership of scrolling.

Right-side details are floating cards inset from every viewport edge, including
safe areas. Their height excludes the top/bottom shell insets and their width
fits the remaining viewport; mobile views keep all four rounded corners. Existing
dialog focus loops, Escape, background scroll locks and focus restoration remain.

`HelpTooltip` and delegated `TitleTooltip` render themed, arrowless rounded panels
through body portals. Legacy HTML title attributes are suppressed while their
card is active and restored when leaving or unmounting; dynamic title changes
remain visible and existing accessible descriptions are preserved. Hover and
keyboard focus expose the same text. Delegated title tips dismiss on Escape
without consuming the key; HelpTooltip closes first while leaving its enclosing
dialog open, so a subsequent Escape can close the dialog.
Positions use viewport coordinates, clamp to viewport edges and remain correct
after scrolling. Tooltip text is never interpreted as HTML. Canvas chart tooltips
use the same rounded panel shape and omit pointed carets; tour popovers also omit
arrows. Existing searchable/multi-select controls retain their integrated behavior.

Custom HTML and external iframe documents retain ownership of their internal
styles. The application provides the rounded embedding card; QR codes retain
their opaque white quiet zone and are not clipped into rounded code pixels.

Date, local date/time and time fields share `DateTimePicker` for their expanded
UI: an opaque themed card with a calendar, local-time controls where applicable,
and clear/cancel/apply actions. Browsing or cancelling does not change a bound
value; applying or clearing preserves the field's existing model and event
contract. Date-only fields remain dates, local date/time values are never shifted
through UTC, and second-resolution time fields retain seconds. Min/max, step,
required, disabled and readonly semantics remain enforced. Any native input is
only a hidden form/constraint proxy, never the visible expanded picker. Calendar
panels escape scrollable cards through body portals, fit the viewport, and share
the dialog focus/Escape stack without independently locking page scrolling.

Error, warning, success and information notifications share a 20px rounded solid
card with a rounded semantic icon well, close button and inset progress track.
Color identifies severity without changing the card shape. The stack follows
the shell's top/right inset, fits narrow screens, and preserves announcement,
timeout and dismissal behavior.

Record tables retain table semantics while presenting each record as a separate
16px rounded card. Hover and selection paint the same rounded row without
filling its 8px gap. Each record has a 1px outline that follows its rounded
corners, with an accent outline on hover or selection. Outer table outlines and
vertical pinned-column shadows are omitted; visible outer insets are 12px.
Data cells, footer totals and frozen-column paint layers have no static fill;
the enclosing card supplies their background at every transparency setting.
This also applies to grouped price tiers in Model Plaza. Hover and selection
add only their shared state tint and outline.
Headers match the surrounding table background, have no independent card, and
remain 12px from the first data row. Header groups and cells add no fill layers
over the enclosing card. A static header rail and a separate body scrollport
prevent vertical overlap without a scroll-driven masking delay. Every constrained
scrolling record table uses this separation, including distribution tables whose
headers were not previously sticky. Long dialog tables have a local, bounded body
viewport; short reference tables retain their natural height. Header sorting,
selection and column visibility stay synchronized with the source table; frozen
columns and horizontal scrolling retain column alignment. At every transparency
setting, horizontal cell clipping prevents content from overlapping frozen
columns. Grouped, spanning and expanded records keep their business grouping;
virtual rows retain accurate measurement.

Short data rows use `record-table-compact` with 10px visible outside corners
instead of the default 16px. This includes model, group and endpoint
distributions, compact rankings and model summaries, including expanded user
breakdowns, price tiers and short reference/monitor tables. The extra-model
table inside monitor tooltips uses `record-table-dense` for 6px corners on its
very short rows. Hover and selected states keep the same corners. Available
channel cards use at most four columns; their actual content width selects one,
two, three or four columns. Redeem balance cards use the ordinary theme surface.

Pagination responds to its containing card rather than the browser width. Wide
cards keep information and navigation on one line; narrower cards put controls
below their information and keep previous/next actions together. Page-size
labels stay on one line. Redeem activity and batch-image footers follow the same
container-based layout rules. Prompt Audit and V2 settings fill the console's
content column. Prompt Audit's save card occupies a fixed layout slot below
configuration; wrapped actions reserve their actual height so the last setting
remains reachable when navigation collapses or the viewport changes.

Navigation hover and keyboard focus paint an inset rounded card. The active
link has a stronger accent fill and outline, retained on hover, without a leading stripe.

The V2 channel pulse matrix mounts only visible rows with a small overscan and
measures actual row heights, including mobile wrapping. Its header remains sticky;
the body clips at the header edge using the saved scroll offset, including after
restoration and filter clamping, so transparent headers never show rows behind them.
Filtering clamps the scroll position and keyboard navigation can reach unmounted
rows. Ordinary wheel input scrolls; Ctrl + wheel zooms the time axis. The pulse
matrix preserves its zoom and scroll after polling or a switch to line view and
back. A change of
range or dimensions resets zoom after the new response arrives. One shared
tooltip follows the pointer without rerendering the matrix or allocating time
formatters on every movement.

V2 settings use a searchable platform rail and responsive model cards with
search, selection, batch actions and undo. Separate tabs show individual models,
actual observed members of Other, and excluded models. Exclusion removes a model
from every V2 statistic; moving it to Other retains its traffic. The integrated
model picker distinguishes observed traffic, configured names and preset
candidates. Draft changes apply only after versioned save succeeds; a conflicting
save retains the draft and offers an explicit reload. See
[the V2 configuration contract](CHANNEL_MONITOR_V2.md).

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

Admin history keeps the visible column prefix through account pinned during
horizontal scrolling. Headers and data use the same measured cumulative widths;
showing the optional API key column includes it in that prefix. Reordering or
hiding columns must never leave a visible account label covered by the user
column. Other tables retain their existing fixed-column choices.

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
set a shared accent color, card background transparency, background image and
independent light/dark canvas masks. These settings apply across both consoles;
each viewer keeps their own light/dark choice. The default accent is `#14b8a6`,
and default transparency is 0% (the existing theme fills). Each color and
transparency control has its own restore-default action. Background reset clears
the image and restores the 30%/48% masks without resetting these other controls.

Accent colors use `#RRGGBB` and synchronize controls, input/select borders and
focus, hover, selected navigation/tabs and menus. Error/health, provider branding
and chart category colors retain their meaning. Transparency ranges from 0%
(opaque) to 100% (transparent) and affects card, control/button, table row,
expanded region, dropdown and popup backgrounds, including chart hover cards.
Text/icons, focus and 1px borders remain visible at every setting. QR quiet zones
and accent picker swatches remain opaque. Table records retain compact rounded
outlines with accent hover, fixed columns/headers clip content behind them, and
scrolling viewports retain rounded corners. Main content shells have no outline;
their nested cards and controls retain visible edges. Settings tabs share
usage tab height (62px normally, 54px in short desktop windows).

Adjustments produce a local draft. Preview affects only that administrator and
ends when leaving General or the settings page. **Save appearance** publishes
this card independently from other general settings. Saving applies immediately
and survives refresh; other viewers receive the published setting on refresh.
Use CSS custom properties and build-time background bindings, never ancestor
`opacity`, extra container filters, or runtime stylesheet traversal. Only table
geometry needed to prevent transparent fixed-column overlap updates on scroll.

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
`dark_mask`, nullable `card_transparency`, optional `accent_color`, and optional
`background_image` (JPEG/PNG/WebP data URL; omitted/null keeps the current image,
empty string removes it). Percentages must be integers in 0–100; legacy null
transparency renders as 0%. Accent is a six-digit HEX color, stored lowercase;
omitted/null keeps the existing accent and empty string resets it to default.
Legacy or damaged stored accent fields fall back to default without discarding
an otherwise valid background or masks. Invalid updates do not write settings.
Responses, public settings and injected `__APP_CONFIG__` expose metadata only:
`background_url`, both masks, `card_transparency` and `accent_color`. The existing
settings JSON stores these values without a migration or environment bindings.
Image bytes are never embedded in these public payloads.

`GET /api/v1/settings/console-background/:revision` publicly serves the active
JPEG with a SHA-256 URL, ETag, `nosniff`, and immutable caching. A replaced or
cleared uncached URL returns 404 with `no-store`; already cached images may
remain in browser caches until expiry. Saving invalidates the HTML settings
cache and immediately updates the administrator's public-settings cache. Other
viewers get the published appearance on refresh. Failed image loads fall back
to the built-in gradient, and stale load completions must not replace a newer
image or a reset. Table bodies, sticky headers and fixed columns use the shared
theme fills with no backdrop filters. When those fills become transparent,
cell-level clipping preserves fixed-column and header readability, rounded
hover/selection boundaries, stacking and focus. Long document-scrolling usage
lists must remain fully painted after repeated vertical jumps.

Console surfaces use the shared tokens in `frontend/src/styles/console.css` and
`surfaces.css`. `surface-card` paints standalone cards, `surface-inset` paints
nested sections, `surface-control` paints buttons, and `surface-panel` owns
dropdown/custom-dialog paint. `surface-overlay` is a separate, lightly
dimmed full-screen scrim without a backdrop filter. Shared Select, date-range,
proxy, dialog and button styles use these same tokens. New controls must retain
their integrated search, checkbox, focus, error and disabled behavior.

The main solid surface is `#f8faf9` in the light theme and `#262c2d` in the dark
theme. Inset sections use `#edf2f0` and `#202627`, respectively. Shared card,
control, panel and table fills share the configured transparency without relying
on backdrop filters. Deep-theme secondary text uses `#c1cdd1`, with distinct primary,
selected and danger colors. Do not dim ancestor opacity or filter containers
holding fixed menus. Preserve opaque switch thumbs, status/progress marks and
QR quiet zones (`surface-opaque`).

The date-range menu retains its trigger alignment when it fits, and otherwise
stays at least 12px from the horizontal viewport edges. Opening, scrolling or
resizing the menu must not apply date changes; Apply retains that responsibility.
