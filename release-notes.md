Sub2API Plus v0.2.24+custom.001

## Highlights

- Synchronizes official Sub2API v0.2.24 with the Plus provider, identity, audit and billing contracts.
- Rejects repeated request members before routing or billing. Preserves the protocol-specific error format and earlier per-route body limits.
- Protects completed OpenAI OAuth rotations from optional enrichment deadlines and keeps tool-level Alpha Search 403s from penalizing credentials.
- Restores buffered Responses output from stream items, preserves Kimi dynamic tools and DeepSeek native Messages thinking history, and adds Grok successful-search accounting and five-source image edits.

## Changed

- Adds source links in compatible Messages search results, Claude window-cost estimates, exact model ID copying and a manual token-refresh frequency hint.
- Admin-created ordinary users receive configured default platform quotas. Bulk edits can opt in to changing the OpenAI passthrough model filter.
- Text-only gateway endpoints enforce their smaller body limit before group middleware reads the body.

## Compatibility and migration

- Application version `0.2.24+custom.001`. Git tag `v0.2.24+custom.001`. GHCR image tag `v0.2.24-custom.001`.
- No database migration or dependency lockfile update is required for this import.
- HTTP gateway JSON with repeated fields now returns 400. A duplicate field in a Responses WebSocket frame closes that connection. Repeated scalar multipart fields also fail validation.
- Plus uses the selected official agent profiles for all seven platforms, including Codex 0.170.0.

## Known issues

- Live provider entitlements depend on the credential owner's plan. Container verification uses synthetic providers for protocol coverage.

## Upstream baseline

Official release: v0.2.24
Official commit: 5c2982a81b46e2142d0143d2f83c1ce9c4da1d24
