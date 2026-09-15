Sub2API Plus v0.2.4+custom.006

## Highlights

Plus now includes the pinned official `main` snapshot after `v0.2.4`: OpenCode as a first-class platform, native Codex Images for OAuth, subscription-surface controls, and bulk admin actions, while keeping Plus identity, ingress audit, async images, and retired billing probes.

## Changed

- OpenCode Zen/GO accounts, session headers, and local Anthropic count_tokens estimates.
- OAuth image requests can use native Codex Images; Grok abandoned media slots are released.
- WebSocket pooling uses execution-scope keys, idle ping, and peer-close eviction after ingress audit.
- Admin bulk API-key edit, bulk subscription actions, selected-user delete, and registration password confirmation.
- Site billing/subscription switch hides user-facing subscription entry points when disabled.
- Antigravity Gemini 3.7/3.8 Flash, Ollama Cloud async rate-limit reset, and ops token stats across platforms.

## Compatibility and migration

Migrations 266 and 267 add OpenCode platform constraints and delete unlimited (all-NULL) user platform quota rows. Back up the database before upgrade. Official still has no `v0.2.5` tag; this release stays in the `0.2.4+custom.NNN` family on snapshot `badfad8b7248b8aac0e6b503a06e392aa31cb294`.

## Known issues

Official `main` remains untagged. A later official release may require another overlay.

## Upstream baseline

Official release: v0.2.4
Official commit: badfad8b7248b8aac0e6b503a06e392aa31cb294
