# Upstream Refusal Guard

Applies only to requests handled by Anthropic `/v1/messages`. Disabled by default; administrators, like other users, are subject to the guard. It runs **after** ingress security audit and before channel mapping, account selection, concurrency and billing. Ordinary upstream responses, failover and passthrough remain unchanged. Already-started streams with `stop_reason=refusal` are not detected.

## Configuration

Setting `upstream_refusal_guard_config` stores JSON. Defaults: `enabled=false`, `user_threshold=3` (1–100), `count_window_seconds=300` (10–86400), `user_cooldown_seconds=300` (10–86400), `session_block_ttl_seconds=2592000` (60–7776000). The settings page expresses session TTL in days and stores seconds. Changes on this process are effective immediately; other processes refresh their settings cache within 60 seconds (read failures retry after 5 seconds).

Each enabled rule has `id`, `name`, `enabled`, optional `status_codes` (empty matches all), `error_codes`, `error_types`, and `message_keywords`. A rule matches when its status matches AND at least one of the other three groups matches. Error code/type comparison ignores case; message keywords are case-insensitive substrings. The code/type/message are read from `error.code`, `error.type`, `error.message` (top-level type/message as fallback). Rules with all three matching groups empty cannot be saved. Defaults: `vm2api_refusal_guard` matches status 400 and code `refusal_guard` or type `policy_blocked`; `anthropic_usage_policy` matches status 400 and message containing `violate our Usage Policy`.

## Enforcement and Redis

On a match the current session is blocked for its configured TTL; the user's count increases by one and its TTL is refreshed to the full count window. When the threshold is reached, the count is cleared and the user cooldown begins. Session blocks do not refresh their TTL. Session lookup takes precedence over user cooldown. A request with no session hash can count toward the user's cooldown without creating a session block.

Keys: `refusal_guard:session:<sha256(tenant|session_id)>`, where tenant is `user:<id>` or, if no user ID, `api_key:<id>`; `refusal_guard:count:user:<id>`; `refusal_guard:cooldown:user:<id>`. Session values include user, API key, truncated session ID (128 characters), request ID, rule ID, model and creation time. Cooldown values include user ID, expiry, triggering request ID and threshold count. Redis errors are fail-open and logged with request ID, user ID, API key ID, endpoint, stage and stable error code (without request content). Redis is the only state store: losing its data clears guard state.

A blocked session receives HTTP 403 with `type=permission_error`, `code=session_blocked_by_upstream_refusal` and a bilingual new-session instruction. A cooling user receives HTTP 429 with `type=rate_limit_error`, `code=user_cooldown_by_upstream_refusal`, a bilingual remaining-seconds message and a `Retry-After` header.

## Administration

Under `/admin/risk-control/refusal-guard/`: `GET` and `PUT config`, `GET users`, `DELETE users/:user_id`, `GET sessions` (page, page_size, optional user_id), `DELETE sessions` with `{"block_key":"<hex sha256>"}`, and `DELETE all`. The settings tab allows editing rules, checking state, clearing user counters and cooldowns, unblocking sessions and clearing all state (with confirmation). Lists scan at most 1000 keys per category.
