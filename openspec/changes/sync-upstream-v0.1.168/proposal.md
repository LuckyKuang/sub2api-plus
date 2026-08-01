## Why

Sub2API Plus currently tracks official `v0.1.166` with intentional security,
deployment, quota, image, and stream-output extensions. Official `v0.1.168`
adds Passkey authentication, Model Plaza, concurrent update protection, and
important provider and prompt-audit fixes. The update crosses public APIs,
persistent data, authentication, routing, and deployment, so it must preserve
Plus security invariants rather than be treated as a mechanical merge.

## What Changes

- Merge official `v0.1.168` (`99c8e4bf7564823bafbab369acab6539e734c1bb`)
  into the Plus codebase and release it as `v0.1.168+custom.001`.
- Add Passkey storage with a Plus-specific forward-only migration numbered
  `196_passkey_credentials.sql`; existing Plus migrations remain immutable.
- Integrate Passkey authentication with the existing global IP access policy:
  successful Passkey login clears an existing source-IP failure streak before
  token issuance. Failed Passkey verification remains protected by endpoint
  rate limiting and does not change password/TOTP auto-block policy.
- Keep Model Plaza disabled by default. When enabled, require authentication
  unless an administrator explicitly opts into public visibility.
- Preserve Plus IP blocking, OpenAI outbound identity/session isolation,
  five-hour quota, async-image controls, and first meaningful stream-output
  timing while accepting upstream gateway, Live, and model compatibility fixes.
- Apply upstream scoped user/API-key update protections and prompt-audit
  encrypted-token recovery without weakening Plus deployment guarantees.

## Capabilities

### New Capabilities

- `upstream-v0.1.168-sync`: Defines the supported upstream baseline,
  Passkey/IP security composition, Model Plaza visibility defaults, migration
  identity, and compatibility rules for the Plus `0.1.168` release line.

### Modified Capabilities

- `stream-first-output-timing`: Upstream Messages and Live changes must retain
  the existing distinction among first event, first token, and first meaningful
  output.

## Impact

- **Database**: Adds only Passkey tables. Existing migrations and checksums are
  unchanged; rolling the application binary back leaves the additional tables
  harmlessly unused.
- **Authentication**: Passkey is opt-in and requires explicit WebAuthn relying
  party configuration. Existing password, TOTP, OAuth, and sessions remain
  supported.
- **Public API/UI**: Adds Passkey and Model Plaza endpoints and user interfaces.
  Model Plaza is disabled by default and does not expose group pricing until an
  administrator enables it.
- **Gateway**: Updates Codex, Claude OAuth, Kimi, OpenAI Messages, and Live
  behavior while retaining Plus safety and measurement semantics.
- **Deployment**: Prompt-audit endpoint tokens require a persistent
  `TOTP_ENCRYPTION_KEY`; `SKIP_SETUP` remains opt-in and must be documented as
  an operator-only escape hatch.
