Sub2API Plus v0.1.182+custom.001

## Highlights

- Import official Sub2API v0.1.182 on top of the completed Plus v0.1.179–v0.1.181 integration.
- Keep Plus Prompt Audit conversation-text selection, admin user support view, IP access master switch, Codex mode-only fingerprint with `device` default, usage TPS, and release tooling.
- Prompt Audit blocking now scans only the latest user turn, so Codex instructions and tool schemas no longer inflate Guard chunks or false-positive jailbreaks.
- Content moderation sends up to 16 current-user images, and a default-off `cyber_policy` auto-ban can immediately disable a regular user or the triggering admin API key.

## Fixed

- Responses Lite now normalizes OAuth, API-key, HTTP-bridge, and WebSocket requests consistently, preserves numeric precision, and pins the supported parallel-tool-call mode.
- OAuth image generation keeps the user's prompt verbatim.
- OpenCode Go account resets, Anthropic cache billing, Antigravity routing, Composite Kimi Code K3 routing, channel-monitor attribution, and payment-result balance refresh include official v0.1.182 fixes.
- Prompt Audit no longer blocks ordinary Codex `hi` turns that only carry harness instructions.
- Two-image user turns are no longer sampled down to one inbound moderation image.

## Compatibility and migration

- Published Plus migrations 224/225/226/228 are unchanged; prefix 227 remains unused.
- Upgrades from v0.1.178+custom.005 apply Plus migrations 229–233. Official v0.1.182 adds no additional SQL migrations.
- Experimental OAuth transport plugins remain disabled by default.

## Known issues

- OAuth transport plugins are experimental and default off.

## Upstream baseline

Official release: v0.1.182
Official commit: 5a7d469622911a6b1291a692376df5fa03f9ac2e
