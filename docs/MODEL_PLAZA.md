# Model Plaza Visibility

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
