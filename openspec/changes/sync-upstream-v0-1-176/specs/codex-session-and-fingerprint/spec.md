## ADDED Requirements

### Requirement: Session access checks must precede fingerprint rewrite

OpenAI OAuth session access SHALL be decided by the Plus account policy
before any Codex fingerprint rewrite. Fingerprint convergence SHALL NOT
grant access, skip a deny, or change outbound identity source
precedence.

#### Scenario: An unauthorized group sends a session identifier

- **WHEN** the selected OAuth account has session sharing enabled and
  the request group is not in `allowed_group_ids`
- **THEN** the builder MUST fail closed before fingerprint IDs are
  applied
- **THEN** no upstream request MUST be sent

#### Scenario: Authorized groups continue a shared session

- **WHEN** two allowed groups of the same user send the same raw session
  identifier through HTTP, passthrough, or WebSocket
- **THEN** Plus namespace resolution MUST still produce the same
  account-scoped continuation identifier
- **THEN** fingerprint rewrite MAY later reduce outbound device or
  thread cardinality without changing that access decision

### Requirement: Fingerprint rewrite is outbound-only

Codex fingerprint modes SHALL rewrite only installation, session, and
thread carriers plus `client_metadata`. Usage-log `session_id` SHALL
remain the sanitized client-original value. User-Agent, Originator, and
Version selection SHALL keep the Plus source precedence.

#### Scenario: Session-mode rewrite on an OAuth account

- **WHEN** an OAuth account uses fingerprint mode `session` or the
  agreed unset default
- **THEN** outbound hyphenated `session-id`, `installation_id`, and
  `thread_id` MAY be replaced with the official derived set
- **THEN** `usage_logs.session_id` MUST still store the client-original
  sanitized identifier
- **THEN** the selected outbound UA source MUST be unchanged

#### Scenario: Client-profile rejection

- **WHEN** an enabled account does not match a built-in or compatibility
  Codex profile
- **THEN** the request MUST be denied locally
- **THEN** fingerprint rewrite MUST NOT run

### Requirement: Unset OAuth accounts use official session mode

An OpenAI OAuth account without a stored `codex_fingerprint_mode` SHALL
behave as official `session`. Only an explicit stored `off` disables
convergence. API-key accounts SHALL stay `off`.

#### Scenario: An existing Plus OAuth account has no fingerprint setting

- **WHEN** the account extra has no `codex_fingerprint_mode`
- **THEN** outbound installation and session carriers MUST be rewritten
  to the account-constant values
- **THEN** outbound thread carriers MUST be derived from the
  client-original `session-id`
- **THEN** `usage_logs.session_id` MUST still store the client-original
  sanitized identifier

#### Scenario: Two Codex clients share one OAuth account

- **WHEN** client A sends `session-id: client-A` and client B sends
  `session-id: client-B` through the same OAuth account
- **THEN** both requests MUST share one outbound `session-id` and
  `x-codex-installation-id`
- **THEN** the two requests MUST keep different outbound `thread-id`
  values

#### Scenario: The same client continues a conversation

- **WHEN** the same client repeats `session-id: client-A` on later turns
- **THEN** outbound `session-id` and `thread-id` MUST stay deterministic
- **THEN** `turn_id` MAY change per request

#### Scenario: An administrator stores off

- **WHEN** `codex_fingerprint_mode` is exactly `off`
- **THEN** outbound fingerprint headers and `client_metadata` MUST pass
  through unchanged
