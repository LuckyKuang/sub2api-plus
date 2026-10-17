Sub2API Plus v0.2.17+custom.001

## Highlights

- Add Gemini explicit context caching for AI Studio, Vertex AI and self-hosted gateway upstreams: `POST/GET/PATCH/DELETE /v1beta/cachedContents` bind every cache to the owning account so a cache reference only routes to the account that created it. Storage is billed from configured or built-in per-token-per-hour prices, mutations are serialized with a lease, and failed mutations roll back.
- Support Vertex AI accounts authenticated with an API Key (including `AQ.` AI Studio keys), let service accounts sync their upstream model catalog, and expose Gemini model list/detail for Agent Platform (Vertex) accounts.
- Ship Gemini 4 Argon model catalogs and billing, forward thinking levels on the Gemini compatibility endpoint and stream the thinking summary back, and correct the 3.6 Flash fallback price.
- Extend Antigravity support: Gemini 4 Argon, preserved tool-result images, native Gemini `functionCall`/`thoughtSignature`, composite `/v1beta` routing, isolated image-model rate limits, and Claude 5.5 tier IDs billed at base rates.
- Adapt Claude Haiku 5.5 across the gateway and wire it into the Responses bridge.
- Retire GPT-5.5 for ChatGPT upstreams, evaluated after public alias projection so aliased catalog entries cannot bypass it, and emulate `web_search` on `/v1/messages` for OpenAI API Key accounts while excluding OAuth accounts.
- Always emit `created_at` on gateway-produced Responses objects, fixing clients such as grok-build that reject a Response without it.
- Make the minimum output-token floor configurable through `gateway.min_output_tokens`.

## Changed

- Isolate Codex imports by workspace so accounts that share a user identifier across workspaces are no longer merged, and surface the backend reason when upstream model sync fails.
- Honor Anthropic protocol base URLs for accounts that do not use the official endpoint.
- Reject composite routes whose OAuth accounts make the target platform ambiguous instead of letting an unclassified request fall into the default Anthropic pipeline.
- Evaluate explicit model-level 401 rules at the gateway failover entry and hand the actual upstream model to error handling on count-token, Bedrock and retry-exhaustion paths.
- Restore the inline account filters on the admin accounts page: type, privacy and group stay visible instead of collapsing behind a "More filters" panel.
- Payments: query and confirm EasyPay notifications before crediting, query Alipay refund status, treat refund query errors as pending, finalize failed pending refunds atomically, keep refund deductions while a refund is pending, validate order types, and load the Stripe client secret from the local order snapshot.
- Fix the Zhipu GLM coding-plan 5-hour and weekly window ordering and recognize the international coding quota endpoints.
- Classify gateway-local errors on Gemini native routes by error type in ops diagnostics.

## Compatibility and migration

Upgrade from `v0.2.15+custom.003` applies the forward-only migrations `287_gemini_cached_contents.sql`, `288_gemini_cached_contents_channel_usage.sql` and `289_gemini_cached_contents_mutation_lease.sql`. Official numbers these `243`–`245`; Plus already uses those prefixes, so the same SQL lands at `287`–`289` and existing migration files and checksums stay immutable. The new tables are additive with no backfill; installations that never create an explicit cache see no data change. Plus migrations `246`–`286` are unchanged from `v0.2.15+custom.003`.

Antigravity outbound identity adopts the official `antigravity/hub/` client family at version `2.23.0` while keeping the Plus-pinned `linux/amd64` system fingerprint. A custom Antigravity User-Agent saved in the retired `antigravity/<version>` form is now an invalid candidate: it falls back atomically to the next identity source and the management API rejects it until it is re-saved as `antigravity/hub/<version> …` or cleared. No data migration is required.

New configuration: `gateway.min_output_tokens` defaults to `128` and must be at least `16`; `gemini_cached_content_max_ttl_seconds` caps explicit-cache TTL and treats a non-positive value as `86400`. Gemini cache storage prices default to the built-in official rates and may be overridden per model. The admin account filter revert keeps existing saved filter values; only the collapsing panel is removed.

Official tag `v0.2.17` still embeds `VERSION=0.2.16` upstream (its version-sync commit landed after the tag). Plus identifies this tree as `0.2.17+custom.001`, and `v0.2.16` is included because it is an ancestor of `v0.2.17`; no separate `v0.2.16+custom` release exists.

## Known issues

A Gemini explicit cache is rejected with `POST /v1beta/cachedContents` when the model has neither a configured `cache_storage_cost_per_token_per_hour` nor a built-in official storage price.

Antigravity `MODEL_CAPACITY_EXHAUSTED` is shared across accounts for the same model: the gateway returns the upstream 503 immediately instead of retrying the same account or switching accounts, while ordinary `RATE_LIMIT_EXCEEDED` waiting and account switching are unchanged.

An account or global profile that pins a custom Antigravity User-Agent in the old `antigravity/<version>` form silently falls through to the next identity source after the upgrade until it is re-saved; only the version import from `antigravity_user_agent_version` is automatic.

The official `v0.2.17` dependency state keeps `xlsx ^0.18.5` because upstream reverted its own SheetJS CDN pin; the packaged viewer therefore continues to rely on the existing security-scan exceptions until a registry-published fix is available.

## Upstream baseline

Official release: v0.2.17
Official commit: 060d0160417782cd962a0c7f460f6cd4565d4916
