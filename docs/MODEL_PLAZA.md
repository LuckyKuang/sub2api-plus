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
