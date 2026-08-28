Sub2API Plus v0.1.183+custom.003

## Highlights

- Retire the upstream billing probe and its public billing endpoint, removing obsolete scheduling and persistence paths.
- Restrict prompt-security auditing to user-authored content while preserving valid long-context metadata and stripping client harness XML from Guard input.
- Keep Codex auto-review billing accurate when an unmapped request observes a Luna response, and remove the misleading prompt-cache hit-rate display.

## Fixed

- Guard no longer receives Codex or Claude harness instructions, system context, reasoning, assistant output, or prompt variables as user text.
- Unmapped Codex auto-review requests retain the auto-review price when the upstream response identifies `gpt-5.6-luna`, without changing administrator-mapped Luna pricing.
- Retired billing-probe routes remain unavailable, and migration tests preserve unrelated account metadata while removing obsolete probe state.

## Compatibility and migration

- The retired `/v1/sub2api/billing` endpoint and upstream billing-probe behavior are no longer available.
- Existing databases are migrated forward by removing obsolete probe state; no manual operator action is required.

## Known issues

None.

## Upstream baseline

Official release: v0.1.183
Official commit: e8cb019fabf8b55199436229044cbf9aa7a82564
