## 1. Baseline and metadata

- [x] 1.1 Mark the published `v0.1.166+custom.010` upstream mapping as published.
- [x] 1.2 Merge official `v0.1.168` and resolve shared composition conflicts.
- [x] 1.3 Set Plus release identity to `v0.1.168+custom.001` and prepare the upstream mapping.

## 2. Persistence and security

- [x] 2.1 Add Passkey DDL as forward-only migration `196_passkey_credentials.sql`.
- [x] 2.2 Regenerate Wire after integrating Passkey, Model Plaza, and Plus providers.
- [x] 2.3 Compose Passkey success with IP-access failure-state clearing and tests.
- [x] 2.4 Require explicit Passkey opt-in and synchronize WebAuthn deployment variables.
- [x] 2.5 Apply scoped user/API-key updates and prompt-audit key recovery with Plus regression tests.

## 3. Gateway and UI

- [x] 3.1 Merge Codex, Claude OAuth, Kimi, Messages, and Live fixes while retaining Plus gateway invariants.
- [x] 3.2 Add Model Plaza with disabled and authenticated-by-default exposure behavior.
- [x] 3.3 Preserve the current internal route through global session-expiry login redirects.
- [x] 3.4 Synchronize Passkey, Model Plaza, settings, and English/Chinese frontend coverage.

## 4. Deployment and verification

- [x] 4.1 Synchronize deployment examples and provider/protocol documentation.
- [x] 4.2 Run migration, backend, frontend, generated-code, OpenSpec, and release-policy checks.
- [x] 4.3 Upgrade the local Apple Containers application with preserved volumes and complete smoke tests.
