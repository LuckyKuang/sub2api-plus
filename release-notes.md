Sub2API Plus v0.1.183+custom.001

## Highlights

- Import official Sub2API v0.1.183 on top of the completed Plus v0.1.182 integration.
- Keep Plus Prompt Audit conversation-text selection, admin user support view, IP access master switch, Codex mode-only fingerprint with `device` default, usage TPS, and release tooling.
- Prompt Audit blocking still scans only the latest user turn; content moderation still sends up to 16 current-user images, and the default-off `cyber_policy` auto-ban remains.

## Fixed

- Codex `session-id` affinity now takes priority over `session_id` on sticky routing and WebSocket session resolution.
- OpenAI sticky sessions keep a one-request capacity spillover temporary, so a full wait queue does not rewrite the durable account binding.
- OpenAI OAuth 429 quota-exhausted responses can pause the account instead of treating every 429 as a same-account retry.
- Responses custom tool-call item IDs stay typed after restore.
- Email rebind adds alias and concurrency guards.
- Kimi concurrency 403 stays recoverable; Antigravity compatible token limits are clamped; channel-monitor v2 composite aggregation uses NULLIF.

## Compatibility and migration

- Published Plus migrations 224/225/226/228 are unchanged; prefix 227 remains unused.
- Upgrades from v0.1.178+custom.005 apply Plus migrations 229–233. Official v0.1.183 adds no additional SQL migrations.
- Experimental OAuth transport plugins remain disabled by default.

## Known issues

- OAuth transport plugins are experimental and default off.

## Upstream baseline

Official release: v0.1.183
Official commit: e8cb019fabf8b55199436229044cbf9aa7a82564
