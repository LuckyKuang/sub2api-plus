# Model Plaza Visibility

## Group display translations

Administrators may set a group's `i18n` map with locale keys and optional
`name`/`description` fields. Keys use normalized BCP 47 casing; duplicate
normalized keys and names longer than 100 characters are rejected. Values
are trimmed and empty entries removed. An omitted map on update preserves
translations; an empty object clears them. Duplicating a group copies translated
descriptions but clears translated names so the new name remains distinguishable.

User-facing group, API-key, usage, subscription, redeem, checkout, model-plaza
and available-channel responses localize display text using `Accept-Language`.
Model Plaza appends `Vary: Accept-Language` while preserving CORS variation.
The highest-priority language matches the full locale,
then its primary language. Each missing translated field falls back to the
original field; a missing language does not select a lower-priority language.
User DTOs omit the translation map. Administrator forms retain the original
fields and map. IDs, access, routing, pricing, quotas and log identity are unchanged.

Changing the interface language refreshes localized read data without reloading
the document. Open forms, payment selections and unsaved drafts remain intact;
checkout refresh changes group display names only. Migration 286 supplies an
empty map for existing groups, preserving their current display until configured.

## Availability

Model Plaza is an optional group-and-pricing showcase at
`GET /api/v1/model-plaza`. It is disabled by default. An administrator must
enable it in System Settings before the route and header entry are available.

When enabled, sign-in is required by default. This remains true when
`model_plaza_require_auth` has no stored value. Anonymous visibility requires
an administrator to explicitly save `model_plaza_require_auth=false`; the
administrative UI presents an exposure warning before that setting is used.

## Browsing models and comparing prices

The public page and the embedded console open in card view. Each distinct
platform and exact model ID has one card. Search matches model IDs across all
visible groups, ignoring case. Choosing a group, group platform or effective
group rate narrows the results. Switching between cards and the grouped price
tables keeps the current search and filters.

Each card names the group supplying its displayed price. For a model offered
by several matching groups, the first group in effective group-rate order
supplies the quote. Channel base prices and independent media rates can differ
between groups. The card shows the standard-period price, the applicable
billing unit and the first tier when tiered pricing applies. The `video`
billing mode quotes a second of video, while `per_request` quotes one request.
Image and video quotes use their respective independent multiplier when enabled,
with a lower bound of zero as in settlement. Copying uses the exact model ID
required by the client.

Opening a model from either view shows pricing details and the same model’s
offers across groups visible to the visitor, including groups outside the
current filters. The detail view provides official reference prices, applicable
tiers and cache prices, and the group’s pricing rules. Site-wide pricing notes
are available above the results when configured by the administrator.

## Data Exposure

Anonymous visitors see only non-exclusive groups, including their names,
models, pricing, and rate multipliers. A signed-in user may additionally see
exclusive groups listed in that user's `AllowedGroups` relationship. This is a
showcase relation, not authorization to call a model: it does not check an
active subscription or grant gateway access by itself.

An invalid supplied JWT is rejected by optional authentication middleware; it
does not silently fall back to an anonymous request. In backend mode, the
existing backend-mode user guard remains in effect. Global IP access control is
applied before this route and cannot be bypassed through optional JWT handling.
