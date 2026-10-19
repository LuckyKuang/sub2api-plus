# Gemini and Vertex gateway behavior

Gemini groups expose the native `/v1beta` protocol. The model discovery endpoints
`GET /v1beta/models` and `GET /v1beta/models/{model}` prefer AI Studio-compatible
accounts when available. Vertex service accounts query the Google publisher-model
catalog in the selected region and return AI Studio-shaped model descriptions.
Vertex accounts authenticated with an API key use the built-in catalog because
the publisher-model listing path requires an access token.

## Explicit context caching

Explicit caches are scoped to the requesting gateway API key and its current
Gemini group. Composite groups do not expose cache management.

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
