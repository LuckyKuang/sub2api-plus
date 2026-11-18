Sub2API Plus v0.2.22+custom.001

## Highlights

- Add an optional group-level forced Ultrafast tier for supported Astra and Sol models. With both group overrides enabled, other models use Fast. With only Ultrafast enabled, other models retain the caller's tier.
- Let OpenAI passthrough accounts restrict selection and available-channel offers by the source keys in their model mapping. The forwarded request retains the caller's model.
- Forward OpenAI upstream SSE keepalive events before the first semantic output so clients can reset their idle timer. Post-output stream errors still update account health and close with an SSE error.
- Parse the new Kimi plan's four independent quota windows (5h, weekly, monthly, monthly code), and stop the group usage rollup rebuild from blocking usage_logs writes.

## Changed

- Bill Vertex Gemini requests by the region actually used (10% on the multi-region default price); add Gemini 4 Hydrogen and Carbon to the pricing catalog.
- Remove unsupported OpenAI tool-schema regex constraints, including Unicode-escaped forms, and preserve Codex `additional_tools` declaration IDs and matching history references.
- When temporary storage is unavailable, bounded first-output staging falls back to in-memory buffering under the same limit; the unbounded safety-buffering stage still fails closed instead of holding plaintext in memory.
- Merge per-user billing lanes to remove the balance-row hot spot, and read hot-path request bodies without a full decode and copy.

## Fixed

- Stream-level error frames on the compatibility paths (`/v1/chat/completions`, `/v1/messages`) now apply the usual account side effects for 401/403/429/529 instead of only recording the error, so a failing account stops being scheduled.
- The Messages compatibility path no longer writes upstream response headers while the prelude is still held, keeping the attempt eligible for failover until content starts.
- Group usage rollups record an expired lower bound and a rebuild marker, so a rebuild no longer has to exclude concurrent `usage_logs` writes.
- Grok server-side searches (`call_id` prefixed `xs_`/`as_`) are no longer handed back as client tool calls, while the same call ID on a non-xAI upstream keeps its normal tool-call semantics.
- Codex identity no longer rewrites the client `prompt_cache_key` per credential owner: the key stays stable across a failover and is resolved once by the tenant-scoped cache authority.
- Commit usage logs with batched charges, preserve DeepSeek per-field pricing overrides in durable snapshots, and align admission holds with settled Ultrafast and Vertex Gemini prices.
- Keep group usage totals on one database snapshot. Preserve newer Fable rate limits and sticky session bindings when an older request finishes.
- Show account cost in both the endpoint chart and its expanded user breakdown when the column is enabled.

## Compatibility and migration

- Application version `0.2.22+custom.001`; Git tag `v0.2.22+custom.001`; GHCR image tag `v0.2.22-custom.001`.
- Official baseline `v0.2.22`, commit `b0fa9f3e6eeab1968a53e1289b69daa4bb40ca2b`.
- Migration `314` adds `groups.force_openai_ultrafast` with a `FALSE` default. Migration `315` adds retention and rebuild state to `usage_group_rollup_state`. Both migrations are additive.
- The new group and account switches default to off. `concurrency.ping_interval` retains its 10-second default and now accepts `0` to disable SSE slot-wait keepalives.

## Known issues

None.

## Upstream baseline

Official release: v0.2.22
Official commit: b0fa9f3e6eeab1968a53e1289b69daa4bb40ca2b
