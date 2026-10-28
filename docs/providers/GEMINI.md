# Gemini and Vertex gateway behavior

Gemini groups expose the native `/v1beta` protocol. The model discovery endpoints
`GET /v1beta/models` and `GET /v1beta/models/{model}` prefer AI Studio-compatible
accounts when available. Vertex service accounts query the Google publisher-model
catalog in the selected region and return AI Studio-shaped model descriptions.
Vertex accounts authenticated with an API key use the built-in catalog because
the publisher-model listing path requires an access token.

When every schedulable Gemini account in the group declares servable model names,
list and detail endpoints use their union, including wildcard declarations.
API Key and service accounts contribute mapping aliases because native generation
applies their mapping targets. OAuth contributes only native self-mappings,
including Google One's default model set; translated OAuth aliases are not advertised.
If any Gemini account is unrestricted or an OAuth mapping has no native declarations,
the selected account supplies discovery. Antigravity candidates and the group's
model allowlist still apply. Built-in Gemini catalogs and account
connection tests now use the current 3.x models; saved custom mappings can still
declare older models explicitly.

`gateway.max_same_account_retries_gemini` (environment
`GATEWAY_MAX_SAME_ACCOUNT_RETRIES_GEMINI`) sets retries for retryable Gemini
upstream failures before account failover, excluding the first attempt. It
defaults to 4, accepts 0 through 10, and 0 disables these retries. Signature
downgrades retain their separate compatibility budget. Existing account error
policies and pool-mode retry settings remain authoritative for their paths.

## Quotas, image permissions and interrupted streams

Vertex service accounts support total, daily and weekly account quota limits.
Configured limits accumulate through the same billing transaction as API-key
and Bedrock account limits, stop scheduling when exhausted, and drive threshold
notifications. An unconfigured limit does not enable quota accumulation.

Gemini, Antigravity and composite groups enforce `allow_image_generation` on
Messages, Chat Completions, Responses and native generation. The check recognizes
image model names and IMAGE response modalities, including channel and account
model mappings. A denied request returns HTTP 400 without account punishment or
failover. `countTokens` is exempt. Existing Gemini/Antigravity groups are enabled
by migration 295; composite groups retain their saved value. New Gemini and
Antigravity groups default enabled, and can be disabled after creation.

A disconnected stream bills only collected usage. Native streams bill delivered
images before a terminal chunk; Messages/Chat streams do not count undelivered
images. Once a terminal chunk arrives, normal image-count semantics apply.
Client disconnection stays visible in Plus completion/timing diagnostics. Live
upstream read failures retain their error and observed partial usage. Truncated
non-EOF trailing lines never overwrite collected native usage. Cached input is
clamped to a nonnegative uncached-input count when a prompt is blocked.

## Explicit context caching

Explicit caches are scoped to the requesting gateway API key and its current
group. Gemini groups and Composite requests resolved to the Gemini supplier
expose the same cache management APIs. Antigravity and other supplier targets
do not support this resource domain. Composite creation resolves the public
model through the Gemini route registry, sends the selected private model
upstream, and retains the original public alias for rate rules and metadata.

| Endpoint | Behavior |
| --- | --- |
| `POST /v1beta/cachedContents` | Create an upstream cache and return a gateway-owned `cachedContents/{id}` name. Requires a model and a supported cache storage price. |
| `GET /v1beta/cachedContents` | List the API key's active caches in the current group; supports `pageSize` and `pageToken`. Does not expose the provider account's cache list. |
| `GET /v1beta/cachedContents/{id}` | Read the API key's active local cache metadata. |
| `PATCH /v1beta/cachedContents/{id}` | Change expiration with `ttl` or `expireTime`; `updateMask` accepts expiration fields only. Other cache content is immutable. |
| `DELETE /v1beta/cachedContents/{id}` | Delete the upstream cache and local binding. Previously charged storage is not refunded. |

Supported owners are official AI Studio API-key accounts, compatible API-key
upstreams explicitly declaring `credentials.explicit_cache_upstream=true`, and
Vertex accounts authenticated with service-account JSON. OAuth and Vertex
API-key accounts do not participate in explicit caching. Compatible upstreams
may declare `credentials.explicit_cache_upstream_max_ttl_seconds` as a lower cap.

Use the returned gateway cache name in native `cachedContent` references. The
gateway resolves ownership and rewrites the name for the provider. A referenced
cache is routed only to its creating account, without session-based rerouting or
failover to another owner. Missing, expired, foreign-key or foreign-group
references are unavailable to the caller.
For compatible upstream gateways, bound-cache 429/5xx errors do not punish the
gateway account. Pool-mode accounts retry only their configured retryable status
codes and retry count, keeping the original credential owner and proxy route;
other cases and exhausted retries return the upstream failure without switching
cache owners.
For Composite aliases, both the public model and its currently resolved model
must match the stored binding. Resource-only operations use the authorized
record and its creating account; they do not infer a provider from a cache ID.
Extensions capture the current request's prices using the stored public model.

Creation charges input tokens and storage for the requested lifetime together.
Storage uses `cache_storage_cost_per_token_per_hour`, with configured prices
taking precedence over built-in model/family defaults and the existing Plus
pricing multipliers applying. Extending expiration charges only added storage
time; shortening or deletion gives no refund. Storage-only extensions do not
incur a model request charge. The gateway checks known charges against balance,
subscription and API-key headroom and tracks unsettled reservations. If creation cannot
afford storage after the provider reports token usage, the gateway removes the
cache and bills only input tokens already consumed.

`gateway.gemini_cached_content_max_ttl_seconds` caps each create/extension and
defaults to 86400 seconds; non-positive values use that default. Creation without
an explicit expiration defaults to one hour, bounded by the effective cap.
Mutations use a lease: concurrent updates conflict, and uncertain or uncommitted
changes retain a recovery intent to restore the previous expiration without
charging the failed extension. Additive storage migrations and upgrade
prerequisites are documented in the [migration guide](../../backend/migrations/README.md).

## Plus audit and identity contracts

Cache management enters the [canonical ingress audit](../SECURITY_AUDIT_CONTENT_COVERAGE.md)
before account selection, concurrency, billing or upstream writes. Cache create
content uses the existing Gemini extraction shared by both audit engines.

Model discovery and cache writes use the credential owner's
[trusted outbound identity](../OUTBOUND_IDENTITY.md). Token acquisition and every
Vertex catalog page share the operation's snapshot. Detached cache cleanup keeps
the same snapshot after client cancellation; recovery resolves the bound owner,
and a different failover account resolves its own identity.
