# Available-channel model catalog

The authenticated `/available-channels` page lists configured models and their
prices in the groups the current user can bind to an API key. It uses the
`available_channels_enabled` setting independently of `model_plaza_enabled`.
The public model plaza and administrator support table retain their existing
contracts. A catalog entry is not a guarantee of live account health, available
quota or successful routing.

## Read API and access

`GET /api/v1/channels/available` preserves the existing channel-array response.
A single, case-sensitive `view=catalog` query selects the catalog. Missing,
empty, unknown or repeated `view` values return the legacy array. Both use the
existing success envelope (`code`, `message`, `data`), JWT authentication and
feature gate. Unauthenticated requests return 401. When disabled, the catalog
returns `data: {groups: [], user_rate_status: "not_requested"}` without reading
channel or user-rate dependencies; the legacy view returns `data: []`.

`GetAvailableGroups` authorizes groups before catalog assembly: standard groups
respect exclusivity and public-group restrictions; subscription groups require
an active subscription, including exclusive subscription groups. Only active
channels contribute models. An authorized group without an active bound channel
or configured models is retained with `models: []`. Each group can belong to at
most one channel; that database constraint is unchanged.

Model enumeration combines the finite channel mapping/pricing union, group
prices, exact account model mappings and enabled exact Composite routes. Prefix
routes apply to finite configured IDs; wildcards never become literal request
IDs. The group public-model allowlist filters every source. Normal groups remain
on their own platform; composite offers retain the resolved concrete supplier.
Global price catalogs supply prices, not additional models. A group without a
channel can still publish its configured routes, group prices or account model
IDs. The request-local channel snapshot is shared by mappings and billing
resolution. Account-derived candidates come from one request-local projection
of authorized group IDs, concrete platform and model mappings, without loading
account credentials beyond those mappings, proxy records or group associations.
Only active, schedulable, undeleted accounts contribute; transient scheduling
and expiry windows remain irrelevant to configured model availability. Repository
errors abort the catalog instead of producing partial results. There is no
server-side shared user-directory cache or provider discovery,
credential refresh, account selection or usage write during this read.

## Catalog DTO

Both views localize group display text after authorization, using the
[group translation contract](MODEL_PLAZA.md#group-display-translations). The
catalog localizes its DTO without mutating the authorized service group or
changing model prices, personal rates, IDs or access decisions.

The `data` object contains `groups` and `user_rate_status`:

- `loaded`: user-rate lookup succeeded, including no overrides.
- `unavailable`: user-rate lookup failed; all model quotes and group summary
  defaults/ranges are null and the UI offers a retry. No personal or group-only
  substitute is presented as the buyer's effective rate.
- `not_requested`: the feature is disabled or there are no authorized groups.

Groups include `id`, `name`, `description`, `platform`, `subscription_type`,
`is_exclusive`, `rate_multiplier`, optional `user_rate_multiplier`, the
`peak_rate_enabled`, `peak_start`, `peak_end`, `peak_rate_multiplier`,
`peak_timezone` fields, independent image/video rate flags and multipliers,
`long_context_pricing_enabled`, and `models`. Exclusive membership, subscription
status and personal rates are independent concepts.
`policy_version` and server `as_of` identify the policy used for the offer quotes.
`rate_summary` contains `status` (`available`, `unavailable` or `empty`),
`policy_version`, `as_of`, and `entries`. Each entry is scoped to one actual
concrete supplier and billing unit and includes `default_quote` (M=1),
`rate_min`, `rate_max`, `model_count` and `has_model_exceptions`. Only configured
public offers contribute; an unused platform-rate rule cannot advertise a new
supplier. Distinct operation-specific media bases can leave the shared default
unknown while model details remain quotable. An invalid policy suppresses all
rate quotes rather than falling back to default 1.

Each model offer contains:

| Field | Meaning |
| --- | --- |
| `offer_key` | Opaque stable key for group, platform, case-insensitive request ID and bound channel; unaffected by names, prices or ordering. |
| `name`, `platform` | Request model ID and concrete platform; copy preserves spelling. |
| `source` | Bound channel's user-visible `name` and `description`. |
| `billing_mode` | `token`, `image`, `video`, `per_request`, or null when unknown. |
| `billing_unit` | `token`, `image`, `request`, `video`, `second`, or `unknown`; never infer units from mode alone. |
| `price_status` | `resolved` or `unknown`, independent of user-rate status. |
| `price_reason` | For unknown prices: `pricing_unavailable`, `request_dependent`, or `unsupported_unit`. |
| `pricing` | Existing whitelisted token/cache/image/per-request fields, reasoning multipliers and resolved intervals; null when unknown. |
| `official_pricing` | Optional same-model token reference, separately displayed without group rates. |
| `long_context_basis` | `whole_request` for resolved token schedules; removed marginal pricing is not restored. |
| `time_pricing` | Optional timezone, weekday restriction and half-open multiplier periods. |
| `media_tiers` | Optional `{label, unit, price}` resolution/size rows; not token thresholds. |
| `service_tier_pricing` | Optional `{name, pricing}` resolved price schedules for priority, flex and model-owned ultrafast. |
| `rate_quote` | Server-resolved `base_multiplier`, separate `platform_multiplier` and `model_multiplier`, their product `policy_multiplier`, `peak_multiplier` and `effective_multiplier`, source, supplier, public model, billing unit, policy version and `as_of`. New quotes use `rate_calculation: product_v2`; unknown personal rates, invalid policies or an unknown unit produce null. |
| `price_time_multiplier` | Current price-card period factor resolved through the billing rules; 1 outside an applicable period. |
| `endpoints` | Optional configured Composite route scope; it does not imply additional native upstream capabilities. |

Arrays for groups, models and pricing intervals remain arrays when empty.
Nullable price fields preserve null and explicit zero. Optional condition arrays
may be absent. Prices are USD per token or per declared original unit, **before**
the group/personal/media rate. The frontend converts token prices to USD per
million and applies `rate_quote.effective_multiplier` and
`price_time_multiplier` exactly once. It does not independently select personal,
media, platform or model policies. No credentials, account
identifiers, upstream URLs, mapping targets or internal channel/pricing IDs are
serialized. Catalog requests incorporate rates; the user page does not join a
separate `/groups/rates` response.

## Pricing semantics

Token prices and context/service tiers use `ResolveContextPricingSchedule` and
its existing pure billing calculations. Context intervals are `(min, max]`, on
input plus cache-write/read tokens. The group long-context switch is respected;
all tokens use the selected tier. Model-owned and configured service-tier prices
are resolved through billing, including field-specific priority rates. Cards
quote Standard service, standard period and base context/media tier. Additional
conditions are shown in details and do not imply provider capability. Peak-enabled
groups and model time schedules display an amber Dynamic billing badge; tiered
prices display a separate blue badge only when the group enables long-context
pricing and the offer contains context or media tiers. Dynamic billing indicates a configured
condition, not that the current request falls inside the peak window. A merged
card says “Some quotes” when only part of its quote set has that condition. Service
tiers start collapsed; official-reference tables remain expanded within an opened
quote, as described below.

Base-context image-token prices also use billing probes for Standard, priority
and flex. A configured zero image-input price follows billing's text-input
fallback; an explicit zero image-output price remains free. Stale per-request
fields on token-mode cards are ignored, as they are by billing.

Ordinary rate is `user_rate_multiplier ?? rate_multiplier`, including zero.
Independent image/video rates override that rate. Subscription peak rules use
the server billing timezone and `[start, end)`. New request quotes apply the
peak factor to every actual billing meter, including independent image/video
bases. The quote's `peak_scope` records `all_meters`; historical accepted task
snapshots without that field retain their `token_only` behavior. Channel time pricing
keeps its own timezone, weekday restriction and second precision. DeepSeek's
model-owned default peak periods are derived from its billing function; catalog
base prices use a standard period rather than the time of the catalog read.
The current `price_time_multiplier` and `rate_quote` carry the separately
resolved period and consumption factors; both share the catalog's `as_of`.
Residual negative rates are clamped to zero, matching billing. Non-finite final
amounts, including conversion overflow, render as unknown instead of a number.

Image size and video resolution prices use the same pure cost functions and
group/channel precedence as gateway usage. Video-mode prices are per second,
with total cost depending on duration and count. Per-request prices remain per
request. Audio or compound dimensions that cannot be represented safely are
marked unknown. Known media models with legacy per-request or mismatched media
cards are request-dependent because the gateway can charge per output or use
resolution-specific overrides; token-configured media keeps its token quote.
Missing prices are never rendered as free; explicit zero is
`$0`. The page does not change billing behavior to match its presentation.

The channel's requested/channel-mapped billing-model policy determines the
price model while the copied ID remains the request ID. Upstream/response-model
policies that require request-time evidence are shown as request-dependent,
without account selection or a provider probe. Cross-group quotes remain
separate even when the model and price match. Size, service and context tiers
are conditions of a quote, not extra cards.

## Navigation and lifecycle

Cards fill the content width using container queries: 1/2/3/4 columns at
0/460/700/940 px, with 20 px gaps. Both themes, sidebar width changes,
long IDs and mobile details are supported; only price tables scroll sideways.

Search is capped at 384 px (and fits narrower screens); sorting and reset follow
it in a left-aligned wrapping row. The directory has only platform filters and
model name/ID search, with no group filters, headings or group counts.

The frontend aggregates the authorized response by concrete platform plus
case-insensitive request model ID. Each identity produces exactly one card;
identical names on different platforms remain separate. The card preserves an
actual request ID for copying. All underlying group quotes, source channels and
pricing conditions are retained in details. Search filters whole model cards,
never removes some quotes from a matched model's comparison.

Cards show per-unit input/output or unit-price ranges after applying each quote's
own effective rate exactly once. Equal endpoints show one price. Input and output
ranges are independent and may have minima from different groups. Distinct units
are displayed separately within one card. Missing, unknown or non-finite
base prices are excluded from ranges, with an incomplete-price
notice; explicit zero remains zero. Failed personal-rate lookup labels ranges as
default-rate references. Minimum displayed price does not select a routing group.

The catalog adapts from one to at most three columns, with 20px gaps and larger
cards with 16px corners. Wrapped platform buttons retain their natural width.
Desktop controls remain fixed above the independently scrolling results.

Details immediately show every authorized quote in the group comparison table.
Each group's complete pricing and billing conditions are available through its
keyboard-focusable disclosure. Quote details and each resolved service tier
(Fast/priority, Flex and Ultrafast) start collapsed; their price tables are mounted
only while expanded. Multiple quotes and tiers can stay open; reopening details
or switching models resets disclosures. Opening details performs no extra API
request and never truncates the authorized quote list.
Collapsed price regions are excluded from the dialog's Tab loop; their summaries
remain reachable, and expanded price regions rejoin that loop.
Official reference prices remain expanded inside an opened quote. Small price
tables keep their native header; only actual vertical overflow separates the
interactive header from its body scrollport. Transparent tables without frozen
columns use native overflow without per-cell layout measurements.
Exclusive membership is purple, an effective personal rate green, an
ordinary group rate blue, and an independent media rate cyan. Personal rates show
the default group rate as secondary text, including personal zero. Media rates
supersede personal rates; an overridden personal rate is explicitly marked. The
API key's bound group determines the actual charge. Exclusive membership and
personal rates remain independent concepts.

Ordinary group rates use compact text such as `1x`. Exclusive membership appears
as a purple inline suffix separated by a space from the group name (or the card's
quote count), rather than a separate badge or line.
Rate labels use 12 px text and compact padding to keep comparison rows short.
Each quote header places its group name, source channel, public/subscription
status and effective rate in one wrapping row, with descriptions below it.

Default filters are all platforms, empty search and name sorting. Search trims
and case-folds model IDs; platform navigation uses the full authorized directory.
Name sorting uses model name, platform and stable model key. Price sorting puts
single-unit models into token/image/request/video/second/unknown buckets and
orders by minimum unrounded effective base price, with unknown prices last in
their bucket. Mixed-unit cards follow single-unit cards and use stable name
ordering instead of comparing unlike units. Counts show unique model cards.
Groups with no models remain in the API response but add no card or navigation
item; an entirely empty model directory displays a no-models message.

Refresh retains valid platform/search/sort filters and updates open details by
model identity. Removing one group quote updates the existing dialog; removing
all quotes for the model closes it and restores focus to refresh. A failed refresh clears stale directory data and details and
shows a retryable error. Request sequence and authenticated user identity guard
against stale completion; logout/user changes clear all previous data. Details
support keyboard focus containment, Escape and focus restoration. Copy uses the
application clipboard helper, including failure feedback and a selectable ID.

Successful catalogs with loaded user rates are reused for 180 seconds in
document memory, isolated by login session, actor, assistance target and locale.
The cache holds at most four scopes and clears on login/logout; reads do not
extend freshness. It never stores tokens or responses in persistent browser
storage. Entry and locale changes may reuse a fresh response; manual Refresh
and Retry always invalidate it and perform a real request. Failed, aborted,
unavailable-rate and superseded responses never populate the cache. Filters
operate locally, with no timer, focus or visibility refresh. Cached quote
timestamps remain visible evidence of their original snapshot.

## Validation

Run focused backend handler/service tests, frontend lint/typecheck, channel and
page Vitest, public-plaza/support regressions and locale completeness inside the
platform container described in [CONTRIBUTING.md](../CONTRIBUTING.md). Browser
checks cover column thresholds, sidebar changes, mobile overflow and keyboard
interaction. This read-only view adds no inference audit input or outbound
identity path; see [security audit coverage](SECURITY_AUDIT_CONTENT_COVERAGE.md).


### API Key group rate disclosure

`GET /api/v1/channels/available?view=key_quotes` returns the same authorized
commercial catalog projection used by Key group selection. It remains available
when the optional Available Channels page is disabled. The `view=catalog` and
legacy list keep their existing page enablement gate. Authentication and
bindable-group permissions apply before model or personal-rate lookup; duplicate
`view` parameters do not select this mode. No account, supplier cost, credential
or private model mapping fields are included.

`rate_summary` groups current quotes by actual platform and billing unit, with a
default quote, model exception range, policy version, quote timestamp and explicit
availability status. Key creation, editing, inline changes and bulk changes use
this server projection. Failed or superseded requests never render a fallback
selling rate. A model without an explicit selling multiplier contributes `M=1`;
a missing base price remains unknown, and an explicit zero remains zero.

Create/edit dialogs show supplier/unit summaries inside the existing searchable
group dropdown and show the chosen group's expandable model details below it.
The inline group-change menu shows the same compact summaries. Bulk group
changes show summaries in each option and model details for the selected target
group. The default is the backend's effective quote with `M=1`, including the
applicable personal/group/media base and peak factor; it is not the raw platform
factor `P`. Model ranges also come from the backend and never merge units.

While the Key page is visible, quotes refresh every 60 seconds and on focus or
visibility restoration. Starting a refresh clears the previous quotes; a failed
request displays unavailable with retry. Aborted or late responses cannot
replace newer quotes. This display is current guidance, while actual usage
retains the request's accepted pricing evidence. A platform or factor rule with
no finite configured public models produces an explicitly empty model catalog.
