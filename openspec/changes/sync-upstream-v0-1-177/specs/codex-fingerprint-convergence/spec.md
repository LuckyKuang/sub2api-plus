## ADDED Requirements

### Requirement: OAuth fingerprint convergence defaults to session

An OpenAI OAuth account SHALL use `session` convergence when
`codex_fingerprint_mode` is missing, empty, invalid, or explicitly `session`.
Only explicit `off` disables convergence. Explicit `device` and `full` SHALL
retain their documented behavior. API-key accounts SHALL stay off.

#### Scenario: Existing account has no valid stored mode

- **WHEN** the mode is missing, empty, or an unknown string
- **THEN** the effective mode SHALL be `session`
- **THEN** frontend create, edit, and bulk controls SHALL display `session`

#### Scenario: Administrator saves a mode

- **WHEN** the selected mode is `session`
- **THEN** persistence SHALL delete `codex_fingerprint_mode`
- **WHEN** the selected mode is `off`, `device`, or `full`
- **THEN** persistence SHALL write that explicit value

### Requirement: HTTP headers and metadata must share one staged fingerprint set

The HTTP builder SHALL derive fingerprint IDs once from client-original
carriers after resolving the credential owner. HTTP headers and map/raw
`client_metadata` SHALL use the same IDs and turn-start timestamp. This
requirement SHALL NOT be interpreted as a WebSocket pooled-handshake guarantee.

#### Scenario: Raw passthrough metadata is rewritten

- **WHEN** an eligible passthrough request contains raw `client_metadata`
- **THEN** its rewritten IDs and timestamp SHALL equal the staged header values
- **THEN** it SHALL NOT obtain an independent current timestamp

#### Scenario: Account selection changes between attempts

- **WHEN** an attempt for account B observes staged IDs created for account A
- **THEN** it SHALL reject or clear those IDs before building the request
- **THEN** no account-A fingerprint carrier SHALL reach account B

#### Scenario: Compact request follows a normal attempt

- **WHEN** native or legacy compaction is built after an attempt with staged IDs
- **THEN** the compact attempt SHALL clear staged IDs
- **THEN** fingerprint headers and metadata SHALL not be converged

### Requirement: Fingerprint mutation must not alter Plus authorization or identity

OAuth session-policy authorization SHALL complete before fingerprint mutation.
Fingerprint handling SHALL NOT change User-Agent, Originator, Version, usage-log
session identifiers, or a session-policy denial.

#### Scenario: Session policy rejects a group

- **WHEN** the selected group is not allowed to use the account's session
- **THEN** the request SHALL fail locally before fingerprint staging
- **THEN** no outbound request SHALL be sent
