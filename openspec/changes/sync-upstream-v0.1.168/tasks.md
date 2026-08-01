## 1. Baseline and metadata

- [ ] 1.1 Mark the already published `v0.1.166+custom.010` upstream mapping as published.
- [ ] 1.2 Merge official `v0.1.168` and resolve shared composition conflicts.
- [ ] 1.3 Set Plus release identity to `v0.1.168+custom.001` and prepare upstream mapping.

## 2. Persistence and security

- [ ] 2.1 Add Passkey DDL as forward-only migration `196_passkey_credentials.sql`.
- [ ] 2.2 Regenerate Wire after integrating Passkey, Model Plaza, and Plus providers.
- [ ] 2.3 Compose Passkey success with IP-access failure-state clearing and tests.
- [ ] 2.4 Apply scoped user/API-key updates and prompt-audit key recovery with Plus regression tests.

## 3. Gateway and UI

- [ ] 3.1 Merge Codex, Claude OAuth, Kimi, Messages, and Live fixes while retaining Plus gateway invariants.
- [ ] 3.2 Add Model Plaza with disabled and authenticated-by-default exposure behavior.
- [ ] 3.3 Synchronize Passkey, Model Plaza, settings, and English/Chinese frontend coverage.

## 4. Deployment and verification

- [ ] 4.1 Synchronize deployment examples and provider/protocol documentation.
- [ ] 4.2 Run migration, backend, frontend, generated-code, and release-policy checks.
- [ ] 4.3 Upgrade the local Apple Containers application with preserved volumes and complete smoke tests.
