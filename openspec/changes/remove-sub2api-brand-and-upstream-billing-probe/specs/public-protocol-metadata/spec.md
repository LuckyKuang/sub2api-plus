## ADDED Requirements

### Requirement: Public responses must not emit private project headers

Locally generated public responses SHALL NOT emit `X-Sub2API-*`. Response-header configuration such as `additional_allowed` MUST NOT allow an upstream or extension to reintroduce reserved project-specific response header names.

Existing standard `Retry-After` and `X-RateLimit-*` behavior SHALL remain unchanged.

#### Scenario: Gateway returns a locally generated quota response

- **WHEN** local quota enforcement responds with limit/reset metadata
- **THEN** the response MUST NOT contain `X-Sub2API-RateLimit-Reset-At` or another `X-Sub2API-*` header
- **THEN** existing `Retry-After` and standard `X-RateLimit-*` headers MUST retain their prior semantics

#### Scenario: Additional response header is configured

- **WHEN** `additional_allowed` contains an `X-Sub2API-*` name
- **THEN** that name MUST remain blocked from the downstream response

### Requirement: Structured technical error metadata must use a neutral domain

System-generated structured error metadata exposed through provider-compatible public APIs SHALL use a neutral technical domain rather than a project-specific internal namespace.

#### Scenario: Google-compatible security error is returned

- **WHEN** the gateway emits Google `ErrorInfo` for a security decision
- **THEN** its domain MUST be `gateway.security`
- **THEN** the change MUST NOT alter security-audit ordering, extraction compatibility, status, or redaction behavior

### Requirement: Product identity and management contracts must remain stable

This cleanup SHALL preserve existing Sub2API/Sub2API Plus product identity and compatibility values, including WebAuthn display name, TOTP issuer, default site name and HTML title, user-visible mail/payment/compliance copy, admin WebSocket `sub2api-admin`, backup types `sub2api-data` and `sub2api-bundle`, export filenames, `SUB2API_API_KEY`, `model_provider = "sub2api"`, browser storage keys, plugin protocols, and internal namespaces.

#### Scenario: Default configuration is loaded after the cleanup

- **WHEN** no custom product display values are configured
- **THEN** WebAuthn, TOTP, site name, and HTML title MUST match their values on the `main` baseline
- **THEN** no default MUST be changed to a generic gateway label

#### Scenario: Existing management client is used

- **WHEN** an existing admin WebSocket client or account backup uses the established protocol/type
- **THEN** `sub2api-admin`, `sub2api-data`, and `sub2api-bundle` MUST remain accepted with existing behavior

### Requirement: Product replacement strings must not be introduced

The implementation SHALL NOT add a generic gateway label as a replacement for a removed technical identifier or existing Sub2API product value.

#### Scenario: Final implementation diff is reviewed

- **WHEN** added lines are compared with `main`
- **THEN** no newly added generic product replacement MUST exist
