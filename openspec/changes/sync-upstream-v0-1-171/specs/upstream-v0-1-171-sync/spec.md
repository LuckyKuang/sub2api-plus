## ADDED Requirements

### Requirement: Official v0.1.171 behavior and Plus release identity must be preserved together

The repository SHALL merge official v0.1.171 commit
f0e7a9c7a23a7d02fb159b62fa809621eb0475a6 while preserving intentional Plus
gateway, security, deployment, pricing, identity, and release behavior. The
resulting Plus release identity MUST be v0.1.171+custom.001 with embedded
version 0.1.171+custom.001 and OCI tag v0.1.171-custom.001.

#### Scenario: Release metadata is validated

- **WHEN** the release metadata, Docker arguments, deployment examples, and
  UPSTREAM.md are checked
- **THEN** the tag, application version, OCI tag, and official baseline MUST
  agree with the declared Plus release

### Requirement: Imported upstream migrations must use unique local prefixes

The official group-profit migration SQL SHALL be imported without modifying
existing local migration files. Imported migrations MUST use unique increasing
prefixes after the current local maximum.

#### Scenario: Local migration prefixes already exist

- **WHEN** the official migrations use a prefix already allocated locally
- **THEN** the repository MUST retain the official SQL under new unused local
  prefixes
- **THEN** existing migrations MUST remain byte-for-byte unchanged

### Requirement: CAPTCHA providers must be integrated as one protected feature slice

Tencent Tianyu and Alibaba CAPTCHA 2.0 support SHALL include provider
configuration, validation, protected authentication flows, administration UI,
English and Chinese locale keys, CSP policy, and deployment examples. Provider
validation failures MUST retain fail-closed semantics.

#### Scenario: An administrator selects a CAPTCHA provider

- **WHEN** the administrator enables Turnstile, Tencent Tianyu, or Alibaba
  CAPTCHA
- **THEN** exactly one configured provider MUST be active
- **THEN** all protected registration, login, OAuth-start, password recovery,
  and passkey entry flows MUST enforce that provider

### Requirement: Official financial and scheduling fixes must retain Plus policy boundaries

The system SHALL preserve official v0.1.171 refund, billing, token-refresh,
scheduler, WebSocket, reset-credit, prompt-audit, model-pricing, and Composite
reasoning corrections. Those corrections MUST NOT bypass Plus authorization,
quota, billing, session-isolation, or account-selection boundaries.

#### Scenario: A refund needs forced confirmation

- **WHEN** a refund exceeds the user's available balance
- **THEN** the API MUST require explicit forced confirmation before the final
  debit and state transition occur in the same transaction
