# Claude / Anthropic

## Model mappings and protocol conversion

Chat Completions and Responses bridges resolve the account's final upstream
model before Anthropic conversion and model-specific input validation. A
public alias mapped to Claude 5.5 receives that model's signed-thinking and
parameter rules. A Claude 5.5 client model mapped to an older Claude model
uses the destination model's rules; the original name cannot prematurely
reject temperature or forced tool use supported by that destination. The
gateway forces upstream streaming after this single conversion and retains
the original client model for downstream protocol behavior and accounting.

## Native reset-credit status

Administrators can query an account's native Claude reset-credit availability
from the account table, or with
`GET /api/v1/admin/accounts/:id/claude/reset-credits`. The route uses the
existing admin authentication middleware. It accepts Anthropic OAuth accounts
whose credential scope contains `user:profile`; API-key, setup-token and other
provider accounts are rejected before any network request.

The service reads
`GET https://api.anthropic.com/api/oauth/usage?cedar_ember=1&skip_spend=1`
using the credential-owning account's OAuth token and configured proxy. It
uses the trusted Claude identity under [Outbound Identity](../OUTBOUND_IDENTITY.md),
captured before token acquisition. HTTP redirects are not followed, preventing
OAuth credentials from being forwarded to another host. This endpoint reads
availability; it does not redeem a credit or reset account limits.

The response projects only `eligible`, `available_count`, `credits`,
`cooldown_until`, `weekly_resets_at`, and UTC `fetched_at`. Each credit exposes
`label`, `resets_left`, optional `starts_at`/`expires_at`, `clears`,
`percent_used`, `blocking`, `use_requires_limit` and `redeemable`. Grant and
organization IDs, selection tokens, credentials and the raw upstream response
are excluded. Missing/null reset-credit data produces an empty credit list;
malformed data produces a typed gateway error.

Only active grants with a valid ID, positive remaining count, a nonempty
cleared-window list and valid time bounds appear. A grant is redeemable only
when eligible, usable now, selected as the next grant, unblocked, outside the
cooldown, and at the limit if required. `available_count` sums the remaining
counts of redeemable grants. Percent-used values outside 0–100 are omitted.

| HTTP status | Error code | Meaning |
| --- | --- | --- |
| 400 | `CLAUDE_RESET_OAUTH_REQUIRED` | Account is not Anthropic OAuth. |
| 400 | `CLAUDE_RESET_PROFILE_SCOPE_REQUIRED` | OAuth scope lacks `user:profile`. |
| 503 | `CLAUDE_RESET_PROXY_UNAVAILABLE` | Configured account proxy cannot be resolved. |
| 503 | `CLAUDE_RESET_TOKEN_UNAVAILABLE` | Token acquisition fails or returns an empty token. |
| 503 | `CLAUDE_RESET_QUERY_FAILED` | Upstream transport request fails. |
| 502 | `CLAUDE_RESET_QUERY_FAILED` | Upstream status is not HTTP 200. |
| 502 | `CLAUDE_RESET_STATUS_INVALID` | Usage/reset-credit payload is invalid. |

Invalid account IDs and repository lookup errors use the existing admin error
handling. Error messages do not include raw upstream payloads or tokens.

## Native reset-credit redemption

Administrators redeem the next available credit with
`POST /api/v1/admin/accounts/:id/claude/reset-credits/redeem`. The route uses
the same admin authentication, audit logging and compliance guard as the
Codex reset-quota route. The request has no payload; the server selects the
grant from a fresh eligibility query. An `Idempotency-Key` is required for
each operator confirmation. Reusing a key replays its stored result without
sending another claim. A new confirmation requires a new key.

The same Anthropic OAuth and `user:profile` requirements apply. After acquiring
an account lease, the service reads `/api/oauth/profile` to resolve a validated
organization UUID, acquires an organization lease shared by duplicate local
accounts, then reads fresh reset-credit availability. It sends a single
`POST /api/organizations/:organization/reset_rate_limits` with the server-selected
grant, program `cedar_ember`, and a deterministic operation identifier. A
successful known outcome triggers a fresh status query for the updated credits.

Token acquisition, OAuth refresh, profile lookup, eligibility query, claim and
status refresh retain the credential-owning account's first trusted identity
snapshot. Account or global settings changes during the operation take effect
on a subsequent operation. Inbound identity headers and SDK defaults cannot
replace that snapshot. All requests render only Claude identity declarations,
including `X-App: cli` and the fixed Stainless fingerprint, through the same
redirect-disabled transport. No cookies or caller session headers are forwarded.

The result contains `outcome`, optional sanitized `reason`, `cleared`,
`cooldown_until`, updated `credits`, and `replayed`. Known outcomes are `reset`,
`already_used`, `not_limited`, `cooldown`, `ineligible` and `unknown`. Only known
reason codes and cleared-window names are returned; grant, organization and
upstream operation IDs and OAuth credentials never appear in the result or
durable result records.

Both durable idempotency storage and account/organization locks are mandatory.
Before sending a claim, the service persists an unknown-outcome organization
fence. Transport errors, unexpected statuses or malformed/unknown results leave
that fence in place: the same confirmation never resends and new confirmations
are blocked for 24 hours. An explicit well-formed `unavailable` outcome fences
the organization for 15 minutes. After the fence expires, a fresh eligibility
query is authoritative. Result-persistence failure returns `unknown` with
`result_persistence_failed`; the pre-send fence remains conservative.
Malformed stored fences, unknown stored outcomes/reasons and missing timestamps
require reconciliation and never authorize a new claim or an unsafe replay.
Accepted operations continue under a 60-second execution deadline independently
of caller cancellation; outcome persistence uses a separate short deadline.
Both deadlines fit within the 90-second account/organization leases.

| HTTP status | Error code | Meaning |
| --- | --- | --- |
| 400 | `IDEMPOTENCY_KEY_REQUIRED` / `IDEMPOTENCY_KEY_INVALID` | Missing or invalid confirmation key. |
| 503 | `IDEMPOTENCY_STORE_UNAVAILABLE` | Durable coordination is unavailable. |
| 503 | `CLAUDE_RESET_LOCK_UNAVAILABLE` | Lock storage is unavailable. |
| 409 | `CLAUDE_RESET_BUSY` | Another claim holds the account or organization lease. |
| 503 / 502 | `CLAUDE_RESET_PROFILE_FAILED` | Profile transport fails or its status/payload is invalid. |
| 502 | `CLAUDE_RESET_ORGANIZATION_INVALID` | Profile contains no valid organization UUID. |
| 409 | `CLAUDE_RESET_NOT_AVAILABLE` | No eligible next grant exists at claim time. |
| 409 | `CLAUDE_RESET_UNRESOLVED` | Previous claim is unconfirmed or its fence needs reconciliation. |
| 409 | `CLAUDE_RESET_UPSTREAM_UNAVAILABLE` | Explicit upstream unavailability is still fenced. |

Query, account-validation and idempotency errors retain their existing typed
responses. A sent claim with an uncertain result returns an `unknown` outcome
instead of encouraging an automatic retry of the irreversible operation.

The admin cell invalidates its previous availability snapshot after a claim.
Unknown outcomes and explicit pre-claim refusals require the operator to query
the count again before opening a new confirmation; the server's durable fence
still decides whether a subsequent redemption is permitted. Transport and
idempotency-in-progress failures retain the confirmation key so a retry replays
the same operation instead of creating another claim.
