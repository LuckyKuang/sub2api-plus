## Why

Official Sub2API v0.1.171 adds CAPTCHA providers, Codex identity and overload
handling corrections, Composite reasoning policy, account-reset cache behavior,
and financial and concurrency fixes. Sub2API Plus is based on v0.1.170 but has
intentional gateway, identity, release, deployment, quota, and Agent Identity
changes on the same paths. A mechanical merge would either drop upstream fixes
or regress Plus guarantees.

The official release also introduces migration prefixes 192 and 193. Those
prefixes are already immutable local migrations, so the imported SQL must be
given new unique prefixes without changing its forward-only behavior.

## What Changes

- Merge official v0.1.171 commit f0e7a9c7a23a7d02fb159b62fa809621eb0475a6
  through a normal three-way merge while preserving intentional Plus behavior.
- Release the merged baseline as v0.1.171+custom.001, with embedded version
  0.1.171+custom.001 and OCI tag v0.1.171-custom.001.
- Integrate Tencent Tianyu and Alibaba CAPTCHA 2.0 across server settings,
  authentication flows, administration UI, locale files, CSP, and deployment
  examples while retaining existing Plus configuration defaults and auditing.
- Preserve the Plus OpenAI outbound-identity resolver as the sole source for
  User-Agent, Originator, and Version; port official Codex version sync,
  normalization, and overload behavior through that resolver.
- Preserve official financial, scheduling, reset-credit, WebSocket, token
  refresh, prompt-audit, and model-pricing corrections with Plus account,
  quota, billing, and session behavior.
- Import the official group-profit migrations under new unique local prefixes,
  then regenerate Ent and Wire from resolved source definitions.

## Capabilities

### New Capabilities

- upstream-v0-1-171-sync: Defines the official v0.1.171 baseline, migration
  import rules, CAPTCHA integration, and Plus release identity.

### Modified Capabilities

- openai-codex-outbound-identity: Adds official client-version synchronization,
  normalization control, and overload behavior without creating a competing
  outbound-identity implementation.

## Impact

- Security: CAPTCHA provider validation, forced refund confirmation, Stripe
  idempotency, and token-refresh race fixes affect public security and payment
  behavior.
- Persistent data: two upstream migrations are imported under new local
  prefixes; existing migrations remain immutable.
- Compatibility: integrations that call the refund API must handle a required
  force-confirmation response when user balance is insufficient.
- Release: no tag, GitHub Release, image, or remote push is created by this
  implementation change. Publication remains an explicit later operation.
