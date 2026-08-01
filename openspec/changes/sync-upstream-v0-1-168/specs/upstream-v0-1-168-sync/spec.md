## ADDED Requirements

### Requirement: Upstream baseline and Plus release identity must remain exact

The repository SHALL map Plus `v0.1.168+custom.001` to official `v0.1.168` at
commit `99c8e4bf7564823bafbab369acab6539e734c1bb`. The embedded application version
MUST be `0.1.168+custom.001`, and the OCI tag MUST be
`v0.1.168-custom.001` without changing the Git/GitHub tag.

#### Scenario: Release metadata is checked

- **WHEN** repository release metadata and deployment examples are validated
- **THEN** the upstream mapping, embedded version, Git tag, and OCI tag MUST agree with the declared identities
- **THEN** the upstream source version MUST NOT overwrite the Plus version

### Requirement: Passkey persistence must use a forward-only Plus migration

The system SHALL create Passkey credential storage through
`196_passkey_credentials.sql`. Existing migration names and contents MUST remain
immutable, and migration `196` MUST remain safe to apply once to an existing
Plus database.

#### Scenario: Existing Plus database upgrades

- **WHEN** a database with migrations through `195` starts the upgraded application
- **THEN** migration `196_passkey_credentials.sql` MUST create the Passkey storage
- **THEN** no earlier migration checksum or filename MUST change

### Requirement: Passkey sign-in must require deployment and administrator opt-in

The system SHALL expose Passkey sign-in only when the WebAuthn relying-party
configuration is valid and the administrator setting `passkey_enabled` is
explicitly `true`. A missing setting MUST behave as `false`. Supported container
deployments MUST forward `WEBAUTHN_ENABLED`, `WEBAUTHN_RP_DISPLAY_NAME`,
`WEBAUTHN_RP_ID`, and `WEBAUTHN_RP_ORIGINS` to the application.

#### Scenario: Valid relying party has no stored switch

- **WHEN** WebAuthn RP configuration is valid and `passkey_enabled` is absent
- **THEN** public settings and Passkey login endpoints MUST report Passkey disabled
- **THEN** an administrator MAY explicitly enable the setting

#### Scenario: Administrator switch exists without valid deployment configuration

- **WHEN** `passkey_enabled` is `true` but WebAuthn configuration is disabled or invalid
- **THEN** Passkey sign-in MUST remain disabled
- **THEN** the database switch MUST NOT weaken the deployment security boundary

### Requirement: Successful Passkey login must clear source-IP failure state before token issuance

After a successful Passkey assertion, the system SHALL clear the source IP's
login-failure state before issuing access or refresh tokens. If the configured
IP access-control state cannot be cleared, login MUST fail closed without
issuing tokens.

#### Scenario: Passkey assertion succeeds

- **WHEN** a Passkey assertion is valid and IP failure state is available
- **THEN** the system MUST clear the source-IP failure streak and record the successful login
- **THEN** the system MAY issue the normal token pair only after that ordering completes

#### Scenario: IP failure state cannot be cleared

- **WHEN** a Passkey assertion is valid but the configured IP state store fails
- **THEN** the request MUST fail closed with the IP access-control unavailable error
- **THEN** no access or refresh token MUST be issued

### Requirement: Model Plaza exposure must be disabled and authenticated by default

Model Plaza SHALL be unavailable when its feature setting is absent or false.
When enabled, it SHALL require authentication unless an administrator explicitly
selects public visibility. Authentication redirects MUST preserve only the
current internal path, query, and fragment as the post-login return target.

#### Scenario: Model Plaza settings are absent

- **WHEN** a client requests Model Plaza and no Plaza settings are stored
- **THEN** the system MUST treat the feature as disabled
- **THEN** no group pricing MUST be exposed

#### Scenario: Enabled Plaza requires login

- **WHEN** an unauthenticated browser opens an enabled, authenticated Model Plaza URL with query or fragment state
- **THEN** the browser MUST be sent to login with that internal URL as an encoded return target
- **THEN** the return target MUST NOT accept an external origin

#### Scenario: Administrator explicitly enables public visibility

- **WHEN** Model Plaza is enabled and `model_plaza_require_auth` is explicitly false
- **THEN** anonymous clients MAY receive only the fields allowed by the Plaza response contract
