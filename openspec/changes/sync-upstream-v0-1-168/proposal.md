## Why

Sub2API Plus previously tracked official `v0.1.166` with intentional security,
deployment, quota, image, and stream-output extensions. Official `v0.1.168`
adds Passkey authentication, Model Plaza, concurrent update protection, and
provider and prompt-audit fixes. The update crosses public APIs, persistent
data, authentication, routing, and deployment, so the Plus security invariants
must be specified instead of treating the update as a mechanical merge.

## What Changes

- Merge official `v0.1.168` (`99c8e4bf7564823bafbab369acab6539e734c1bb`)
  and set the Plus release identity to `v0.1.168+custom.001`.
- Add Passkey storage with forward-only migration
  `196_passkey_credentials.sql` and preserve all existing Plus migrations.
- Require both valid WebAuthn deployment configuration and an explicit
  administrator switch before Passkey login is exposed. Forward the WebAuthn
  variables in every supported container deployment.
- Clear an existing source-IP login failure streak after a successful Passkey
  assertion and before issuing tokens, failing closed when that state cannot be
  cleared.
- Keep Model Plaza disabled and authenticated by default, while preserving the
  requested internal route across a required login.
- Preserve Plus IP blocking, OpenAI outbound identity/session isolation,
  five-hour quota, async-image controls, and strict first-token/first-output
  timing while accepting upstream gateway, Live, and compatibility fixes.
- Apply upstream scoped user/API-key update protections and prompt-audit
  encrypted-token recovery without weakening Plus deployment guarantees.

## Capabilities

### New Capabilities

- `upstream-v0-1-168-sync`: Defines the supported upstream baseline, release and
  migration identity, Passkey/IP security composition, and Model Plaza
  visibility behavior for the Plus `0.1.168` line.

### Modified Capabilities

- `stream-first-output-timing`: Requires upstream provider and protocol changes
  to retain the existing distinction among legacy first event, strict first
  token, and first meaningful output.

## Impact

- **Database**: Adds Passkey tables only. Existing migrations and checksums stay
  unchanged; rolling back the application binary leaves the tables unused.
- **Authentication**: Passkey is opt-in at both deployment and administrator
  layers. Password, TOTP, OAuth, and existing sessions remain supported.
- **Public API/UI**: Adds Passkey and Model Plaza endpoints and user interfaces.
  Model Plaza exposes no group pricing until explicitly enabled.
- **Gateway**: Updates Codex, Claude OAuth, Kimi, OpenAI Messages, and Live while
  retaining Plus access-control and measurement semantics.
- **Deployment**: WebAuthn variables are available in Compose and Apple
  container examples. Prompt-audit endpoint tokens require a persistent
  `TOTP_ENCRYPTION_KEY`; `SKIP_SETUP` remains a recovery-only escape hatch.
