Sub2API Plus v0.2.5+custom.001

## Highlights

First Plus release on official `v0.2.5`. It keeps Plus identity, ingress audit, session/quota accounting, and retired billing probes, while aligning Codex OAuth outbound with the official CLI and importing the official DeepSeek, quota-window, Responses sequence_number, Antigravity SSE, and API-key provider-filter fixes.

## Changed

- Default Codex originator is `codex_cli_rs`. Inference still sends the Plus User-Agent, Originator, and Version triple with the Ubuntu fingerprint.
- OAuth authorize, raw token exchange, JSON refresh, device-code, and revoke follow official Codex clients. Login no longer PATCHes ChatGPT training.
- `off`/`device` fingerprint modes emit official `session-id` and `thread-id` only; `session`/`full` still emit legacy aliases.
- Shared upstream capacity shed (OpenAI overloaded/slow_down, Anthropic 529, Grok model capacity, Antigravity MODEL_CAPACITY_EXHAUSTED) returns immediately without same-account retry or failover. Codex-fatal `server_is_overloaded` is still rewritten to `server_error`.
- Official v0.2.5 DeepSeek empty-mapping whitelist, canonical Codex 5h/7d quota reads, Responses `sequence_number` emission, Antigravity Gemini SSE separator fix, and API-key provider filtering.
- OpenCode Zen/GO accounts, native Codex Images for OAuth, WebSocket execution-scope pooling, and bulk admin actions from the unpublished 0.2.4 overlay.

## Compatibility and migration

Migrations 266 and 267 add OpenCode platform constraints and delete unlimited (all-NULL) user platform quota rows. Back up the database before upgrade. Rollback image is `v0.2.4+custom.005`.

## Known issues

None.

## Upstream baseline

Official release: v0.2.5
Official commit: 86f93c28ee34cc74b629dafb748bd5ac5ca8c5ea
