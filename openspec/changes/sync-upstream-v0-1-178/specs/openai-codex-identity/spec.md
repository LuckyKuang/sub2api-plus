## ADDED Requirements

### Requirement: Credential-owner identity precedence remains immutable

Codex outbound identity SHALL select a valid credential-owner
`credentials.user_agent` before a valid global user-agent and finally the
compiled default. Upstream merges, generic header overrides, retries, probes,
and request classification SHALL NOT bypass this order.

#### Scenario: A shadow account forwards a request

- **WHEN** the shadow account uses credentials owned by another account
- **THEN** the owner's valid identity SHALL be selected before global/default
  values
- **THEN** User-Agent, Originator, and Version SHALL remain coherent

### Requirement: Fingerprint and prompt-cache identity are composed safely

Eligible HTTP requests SHALL stage one fingerprint set after account selection.
Final prompt-cache/session alignment SHALL occur afterward. Native and legacy
compaction requests SHALL preserve their compact identity and SHALL not inherit
ordinary body-cache convergence.

#### Scenario: A compact request follows a normal request

- **WHEN** a compact request is built after a request with staged fingerprint
  values
- **THEN** the compact request SHALL clear staged fingerprint carriers
- **THEN** its raw compact body/session identity SHALL remain unchanged

#### Scenario: OAuth Messages omits instructions

- **WHEN** the compatibility bridge transforms an OAuth Messages request without
  an `instructions` member
- **THEN** the bridge SHALL send an explicit empty `instructions` value rather
  than infer a default developer guard
