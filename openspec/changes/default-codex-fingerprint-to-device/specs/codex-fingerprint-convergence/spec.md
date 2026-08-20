## RENAMED Requirements

- FROM: `### Requirement: OAuth fingerprint convergence defaults to session`
- TO: `### Requirement: OAuth fingerprint convergence defaults to device and is explicit`

## MODIFIED Requirements

### Requirement: OAuth fingerprint convergence defaults to device and is explicit

A real OpenAI OAuth credential account SHALL persist exactly one valid
`codex_fingerprint_mode`: `off`, `device`, `session`, or `full`. New accounts
and legacy accounts with a missing, null, empty, or invalid mode SHALL use and
persist `device`. API-key, setup-token, and credential-shadow accounts SHALL
remain outside this setting.

#### Scenario: A new OAuth account omits the mode

- **WHEN** any supported account creation or synchronization path creates a
  real OpenAI OAuth credential account without `codex_fingerprint_mode`
- **THEN** persistence SHALL store `codex_fingerprint_mode` as `device`
- **THEN** the returned and subsequently exported account SHALL expose `device`

#### Scenario: Administrator saves a mode

- **WHEN** the administrator selects `off`, `device`, `session`, or `full`
- **THEN** create, edit, and enabled bulk controls SHALL submit that exact value
- **THEN** persistence SHALL retain that value instead of deleting the key

#### Scenario: Existing account has no valid stored mode

- **WHEN** the migration observes a real OpenAI OAuth credential account with a
  missing, null, empty, or invalid mode
- **THEN** it SHALL store `device`
- **THEN** runtime forwarding SHALL use device-only convergence

#### Scenario: Runtime observes malformed legacy state

- **WHEN** forwarding observes a missing or invalid mode despite persistence
  enforcement
- **THEN** it SHALL use `device` without writing to the database

#### Scenario: Writer submits an invalid explicit mode

- **WHEN** an application writer submits an unknown non-empty string or a
  non-string value for a real OpenAI OAuth credential account
- **THEN** the write SHALL be rejected
