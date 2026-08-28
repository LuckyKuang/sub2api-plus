## Why

The gateway currently exposes a private billing-interop protocol and can use an upstream-declared rate to overwrite manually managed account rates. That private probe and synchronization capability is no longer wanted. Existing local multiplier-based behavior, including group profit control, remains valid business functionality.

Separately, a small set of system-generated protocol fields sent to third-party upstreams, and private metadata returned by public APIs, reveal implementation-specific `sub2api` identifiers. Those protocol fingerprints should be removed without renaming the Sub2API/Sub2API Plus product or breaking existing management contracts.

## What Changes

- Delete `GET /v1/sub2api/billing`, the upstream billing probe runner, probe persistence, admin APIs/UI, and automatic rate synchronization. Preserve `accounts.rate_multiplier` as an ordinary manually managed rate.
- Add a forward-only migration that removes retired probe data/settings without changing manual account rates, group rates, or profit-control configuration.
- Remove unnecessary `sub2api` identifiers only from system-generated third-party URL/query/header/default-body fields. Do not rewrite user prompts, tool arguments, model output, or payment/product copy.
- Remove private `X-Sub2API-*` metadata from public responses and prevent configuration from re-enabling those response header names. Preserve standard `Retry-After` and `X-RateLimit-*` behavior.
- Preserve request-level Grok client-tool cache behavior under the neutral local-only `X-Grok-Client-Tool-Cache` control, and prevent that control from reaching any upstream transport.
- Preserve Sub2API/Sub2API Plus product identity and compatibility contracts, including WebAuthn/TOTP defaults, site/title/mail/payment/compliance copy, `sub2api-admin`, backup types, export filenames, generated client names, browser storage keys, plugin/internal namespaces, and model-provider identifiers.

The implementation MUST NOT introduce a generic gateway label as a replacement product name.

## Capabilities

### New Capabilities

- `upstream-billing-probe`: retirement of billing interop, probe persistence, admin APIs/UI, and import-boundary discard.
- `outbound-protocol-identity`: narrow removal of system-generated project identifiers sent to third-party systems.
- `public-protocol-metadata`: removal of private branded public-response metadata while preserving product and management contracts.

## Impact

- Public API: former billing and probe routes become ordinary 404s; local public responses no longer include `X-Sub2API-*` headers.
- Persistent data: a new migration removes probe extra keys/settings without changing account, group, user, or model multipliers or profit-control fields.
- Runtime: schedulers and profit control consume only the remaining manually managed account rate; unrelated local scheduling behavior remains unchanged.
- Frontend: account probe controls disappear; manual account-rate and group profit-control controls remain.
- Generated code: Wire output is regenerated after probe lifecycle providers are removed.
