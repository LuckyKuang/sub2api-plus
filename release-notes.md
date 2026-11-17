Sub2API Plus v0.2.22+custom.001

## Highlights

- Add a group-level forced Ultra Fast tier for the models that support it (GPT-6 Astra family); it coexists with forced Fast, which stays the fallback for every other model.
- Let OpenAI passthrough accounts be selected by model, using the account model whitelist and mapping source names; mapping targets still never rewrite the passthrough request model.
- Keep long reasoning streams alive: upstream keepalive events are forwarded before the first semantic output so the client idle timer is not reset by silence, and post-output stream errors still update account health and close with an SSE error.
- Parse the new Kimi plan's four independent quota windows (5h, weekly, monthly, monthly code), and stop the group usage rollup rebuild from blocking usage_logs writes.

## Changed

- Bill Vertex Gemini requests by the region actually used (10% on the multi-region default price); add Gemini 4 Hydrogen and Carbon to the pricing catalog.
- Sanitize tool-schema patterns that Python `re` rejects before forwarding, and preserve Codex `additional_tools` declaration IDs while aligning history references with them.
- When temporary storage is unavailable, bounded first-output staging falls back to in-memory buffering under the same limit; the unbounded safety-buffering stage still fails closed instead of holding plaintext in memory.
- Merge per-user billing lanes to remove the balance-row hot spot, and read hot-path request bodies without a full decode and copy.

## Fixed

- Stream-level error frames on the compatibility paths (`/v1/chat/completions`, `/v1/messages`) now apply the usual account side effects for 401/403/429/529 instead of only recording the error, so a failing account stops being scheduled.
- The Messages compatibility path no longer writes upstream response headers while the prelude is still held, keeping the attempt eligible for failover until content starts.
- Group usage rollups record an expired lower bound and a rebuild marker, so a rebuild no longer has to exclude concurrent `usage_logs` writes.
- Grok server-side searches (`call_id` prefixed `xs_`/`as_`) are no longer handed back as client tool calls, while the same call ID on a non-xAI upstream keeps its normal tool-call semantics.
- Codex identity no longer rewrites the client `prompt_cache_key` per credential owner: the key stays stable across a failover and is resolved once by the tenant-scoped cache authority.

## Compatibility and migration

- Application version `0.2.22+custom.001`; Git tag `v0.2.22+custom.001`; GHCR image tag `v0.2.22-custom.001`.
- Official baseline `v0.2.22`, commit `b0fa9f3e6eeab1968a53e1289b69daa4bb40ca2b`.
- New migrations: `314` adds `groups.force_openai_ultrafast` (default `FALSE`, so existing behavior is unchanged) and `315` adds the expired-lower-bound and rebuild columns to `usage_group_rollup_state`. Both are additive, so a rollback to the previous binary keeps working.
- New switches default to off; no configuration change is required.

## Known issues

None.

## Upstream baseline

Official release: v0.2.22
Official commit: b0fa9f3e6eeab1968a53e1289b69daa4bb40ca2b
