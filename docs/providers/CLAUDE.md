# Claude / Anthropic

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
