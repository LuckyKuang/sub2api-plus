Sub2API Plus v0.2.0+custom.001

## Highlights

- Imported official Sub2API v0.2.0 while preserving Plus security-audit ordering, Codex identity precedence, billing, routing, and deployment contracts.
- Added per-group OpenAI reasoning-effort over-limit handling and Force Fast / Free Fast controls.
- Added interval-aware one-hour cache-write pricing for channel and account-stat billing.
- Added native compaction and requested reasoning-effort usage visibility, plus per-user public-group restrictions.

## Changed

- Added Codex automation and delegation bootstrap normalization after the immutable ingress security-audit gate.
- Added upstream session isolation for Chat Completions compatibility forwarding without weakening Plus tenant isolation.
- Added Kimi-native Responses routing, fallback sanitization, and updated model/reasoning policy support from the official baseline.

## Fixed

- Preserved Plus prompt-cache compatibility behavior while incorporating official API-key cache identity fixes.
- Preserved Plus WebSocket turn pricing, capability, security-audit, and account-attribution behavior while adopting official reasoning-policy fields.
- Updated API-key authentication snapshots to include both Plus billing fields and the official Free Fast group field.

## Compatibility and migration

Database migrations 238 through 244 add native compaction and requested reasoning-effort usage fields, per-user public-group restrictions, one-hour cache-write pricing, and group Force Fast, reasoning over-limit, and Free Fast controls. Official v0.2.0 migrations were assigned new Plus migration identities 241 through 244 so already deployed Plus migrations remain immutable and execute first.

## Known issues

None.

## Upstream baseline

Official release: v0.2.0
Official commit: aa236488351eb71e120fc2b6fb32e36b0374c918
