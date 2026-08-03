## ADDED Requirements

### Requirement: Every eligible Codex upstream operation must use one resolved identity

The system SHALL resolve an OpenAI Codex outbound identity from the
credential-owning account custom User-Agent, then the administrator global
setting, then the compiled default. The value MUST produce a valid paired
Originator and version. Each logical operation MUST reuse its resolved identity
for all nested requests and retries.

#### Scenario: An account has a valid custom Codex User-Agent

- **WHEN** an OpenAI account or its Spark shadow is selected for an eligible
  upstream operation
- **THEN** the credential-owning account User-Agent MUST be used
- **THEN** the paired Originator and Version MUST match that User-Agent

#### Scenario: An account has no custom User-Agent

- **WHEN** an eligible request has no valid account-level value
- **THEN** the global configured value MUST be used when valid
- **THEN** otherwise the compiled default MUST be used

### Requirement: OAuth, PAT, reauthorization, and models must retain account identity

The system MUST use the same account-resolved identity for OAuth token refresh
and subsequent account enrichment, existing-account OAuth code exchange, PAT
revalidation, Alpha Search PAT enrichment, and OAuth Codex models requests
where an existing account is known.

#### Scenario: An existing account is reauthorized

- **WHEN** an administrator starts OAuth reauthorization for an existing
  account
- **THEN** the opaque server-side OAuth session MUST bind the account context
- **THEN** code exchange and follow-up enrichment MUST use that account's
  identity
- **THEN** replacing token credentials MUST preserve the account's validated
  User-Agent configuration

#### Scenario: An OAuth models manifest is fetched

- **WHEN** an OAuth Codex models manifest is requested
- **THEN** its `client_version` query and Version header MUST equal the
  selected identity version

### Requirement: Account identity configuration must be safe and manageable

The account UI and account write APIs SHALL expose an optional Codex User-Agent
for eligible OpenAI credential-owning accounts. Empty values inherit. Invalid
values MUST be rejected on create, update, and import. Spark shadows MUST
inherit their parent identity rather than creating an independent override.

#### Scenario: An administrator saves an invalid account User-Agent

- **WHEN** an account create, update, or import contains an unsupported Codex
  User-Agent
- **THEN** the request MUST fail validation without persisting the value

### Requirement: Untrusted request data must not replace the resolved identity

Inbound headers and generic account header overrides SHALL NOT override the
resolved Codex User-Agent, Originator, Version, OpenAI-Beta, X-Codex headers,
session ID, or conversation ID.

#### Scenario: A request includes a conflicting User-Agent override

- **WHEN** an inbound request or generic account header override specifies a
  conflicting identity header
- **THEN** the final upstream request MUST retain the resolved trusted identity
