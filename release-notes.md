Sub2API Plus v0.2.23+custom.001

## Highlights

- Serve the Codex model catalog from official Codex `rust-v0.170.0` itself: fourteen descriptors, including the new GPT-6.2 Sol and Astra entries, replace the previous per-model extracts and the values the gateway used to synthesize. Context and maximum windows, priorities, reasoning levels, truncation policy and model messages now come from the release instead of being maintained by hand.
- Align the Codex WebSocket contract with that release: honor an upstream-declared `x-codex-websocket-max-message-bytes` instead of inventing a default, keep the per-kind response item IDs the client allocates (`msg_`, `rs_`, `fc_`, `lsh_`, `ctco_` and friends) instead of stripping them, and keep `context_window_id` in its model-facing display form.
- Price the OpenAI Ultra Fast tier per channel. A channel model card can now set `ultrafast_multiplier` independently of Fast/priority and Flex; an unset value keeps the model's published default (6×, or 3× for GPT-6.2 through February 2027) instead of falling back to the generic 2× tier factor.
- Support GPT-6.2 Sol and Astra end to end: published model IDs with every local effort/compact spelling, the official token rates, the request-instant Ultra Fast multiplier (3× through February 2027, then 6×), and the Pro-plan Ultra Fast entitlement that an older account snapshot may not advertise yet.
- Rewrite cache accounting around one authoritative cache-write total. Compatible providers that replay zeroed cache fields, report only a TTL split, or report only part of it no longer cause under-billing, and the three Anthropic buckets keep summing to the provider's own prompt total.

## Changed

- Codex catalog policy stays with Plus and is never written back into the official file: administrator-configured models remain listed, the official service tiers keep their published speed wording while Plus appends the ultrafast tier, the announced shell surface remains `unified_exec`, and image input is advertised only for models Plus has verified.
- API-key routes no longer inherit the catalog's Responses Lite and `code_mode_only` declarations, which describe ChatGPT OAuth semantics.
- Buffered conversions (Responses, Chat Completions and both native-Anthropic bridges) keep the cache-write split and the reconciled buckets instead of reporting a flattened zero.
- An OpenAI stream that goes idle after model output starts keeps the existing `stream_timeout` behavior; only the pre-output case now fails over.

## Fixed

- A later `message_delta` that repeated zeroed cache fields erased the write reported by `message_start`, and a partial TTL breakdown left the unclassified remainder unbilled. Both classes are now charged, with the unclassified remainder in the 5m class.
- A cache-only update no longer leaves the uncached input bucket stale: `input + cache_read + cache_write` is recomputed from the provider's remembered prompt total.
- Anthropic-native upstreams routed through the OpenAI gateway kept no 5m/1h split, so every write was priced as the cheaper 5m class even when the 1h price was configured.
- The stream idle deadline followed the ticker phase instead of the last upstream read, so a 1-second interval could take two seconds to fire. It now tracks the last read, and a pre-output stall fails over once instead of walking the whole account pool.

## Compatibility and migration

- Application version `0.2.23+custom.001`; Git tag `v0.2.23+custom.001`; GHCR image tag `v0.2.23-custom.001`.
- Official baseline `v0.2.23`, commit `5af9afb588c4b014b3df9db7d4073af1f8ae01b6`.
- Migration `316` adds `channel_model_pricing.ultrafast_multiplier` as a nullable `NUMERIC(12,6)` with a positive-value check. It is additive and re-runnable; existing cards keep their Fast/Flex configuration and the model default.
- The embedded Codex catalog is byte-pinned by SHA-256; refreshing it for a new Codex release means replacing the file with the tag's exact bytes and updating the pinned digest.
- The gateway cannot relay `x-codex-websocket-max-message-bytes` on its own upgrade response: the downstream upgrade is accepted before an account is selected, while the declaration arrives with the upstream handshake that follows the client's first frame. An oversized turn is served over HTTP exactly once, and the official client learns the cap from the size close rather than skipping the WebSocket upload. Recorded in `docs/protocols/CODEX_CLIENT_PROFILES.md`.

## Known issues

- None.

## Upstream baseline

Official release: v0.2.23
Official commit: 5af9afb588c4b014b3df9db7d4073af1f8ae01b6
