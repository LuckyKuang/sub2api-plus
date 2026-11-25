Sub2API Plus v0.2.24+custom.002

## Highlights

- Unifies OpenAI/Codex public error text: HTTP, SSE, WebSocket, Live and CLI-bridged failures now return a platform-generated type/code/message, strip upstream diagnostics and error headers, and keep retry, billing, audit and administrator Ops evidence. A 76-key catalog is editable from the console with revision-checked compare-and-set APIs.
- Adds plugin manifest schema v2 capabilities: `outbound.transport.v1` selects at most one sender for the actual credential owner across concrete platform and OAuth/API-key scopes, and declarative `request.instructions.prepend.v1` packages contain only a signed manifest with no runtime payload.
- Adds Zhipu GLM-5.4 Coding Plan support, including explicit low/high/max reasoning levels and platform-aware pricing that no longer resolves through the legacy GLM-5 family card.
- Separates the admin console from the user portal and rebuilds the available-channels catalog with a unified filter toolbar, directory layout and on-demand price disclosures.
- Adds a browser availability test for user API keys.

## Changed

- Settles OpenAI Fast requests by the final outbound tier for OAuth, setup-token and API-key credentials, and exposes the normalized billing service tier in usage DTOs and cost tooltips.
- Reads redeemed codes inside the transaction and returns localized failure reasons, reporting an unknown outcome on network interruption.
- Keeps mobile Ops analysis charts and the health card stable by giving each chart a definite plot region.
- Synchronizes the provider, plugin, channel and protocol documentation with the new behavior.

## Compatibility and migration

- Application version `0.2.24+custom.002`. Git tag `v0.2.24+custom.002`. GHCR image tag `v0.2.24-custom.002`.
- Requires database migration `317_plugin_bindings_transport_exclusive.sql`, which narrows the single-enabled-scope index to transport capabilities so declarative request instructions may coexist on one scope.
- No environment variable, configuration key or dependency lockfile change is required.
- Error bodies for OpenAI/Codex paths are platform-generated; stored originals remain available to administrators. Retry, failover and billing semantics are unchanged.
- Plus keeps its provider-owned identities, canonical ingress audit boundary and frozen billing contracts.

## Known issues

- Plugin transport verification still omits native Gemini/Antigravity and WebSocket paths; these are documented as remaining gaps.
- Live provider entitlements depend on the credential owner's plan. Container verification uses synthetic providers for protocol coverage.

## Upstream baseline

Official release: v0.2.24
Official commit: 5c2982a81b46e2142d0143d2f83c1ce9c4da1d24
