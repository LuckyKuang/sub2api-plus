## ADDED Requirements

### Requirement: Credential-owner identity precedence remains immutable

Codex outbound identity SHALL select a valid credential-owner
`credentials.user_agent` before a valid global user-agent and finally the
compiled default. Upstream merges, generic header overrides, retries, probes,
Fast `service_tier` wiring, and request classification SHALL NOT bypass this
order.

#### Scenario: A shadow account forwards a request

- **WHEN** the shadow account uses credentials owned by another account
- **THEN** the owner's valid identity SHALL be selected before global/default
  values
- **THEN** User-Agent, Originator, and Version SHALL remain coherent

### Requirement: Fingerprint remains mode-only with device default

Eligible HTTP and WebSocket requests SHALL resolve one mode-only fingerprint
set after account selection using the latest Plus implementation. The default
mode SHALL remain `device`. Identity SHALL derive from the credential-owning
account ID. Official seed persistence SHALL NOT return.

#### Scenario: A compact request follows a normal request

- **WHEN** a compact request is built after a request with fingerprint values
- **THEN** the compact request SHALL clear request-scoped fingerprint state
- **THEN** its raw compact body/session identity SHALL remain unchanged

#### Scenario: A new Codex OAuth account is created without an explicit mode

- **WHEN** the account is persisted without an operator-selected fingerprint
  mode
- **THEN** the stored mode SHALL be `device`
