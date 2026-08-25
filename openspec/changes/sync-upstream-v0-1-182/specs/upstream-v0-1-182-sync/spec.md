## ADDED Requirements

### Requirement: Official v0.1.182 and Plus release behavior coexist

The repository SHALL integrate official v0.1.182 commit
`5a7d469622911a6b1291a692376df5fa03f9ac2e` while retaining intentional Plus
identity precedence, audit ordering, fingerprint, deployment, and distribution
behavior. Prepared metadata SHALL identify `v0.1.182+custom.001` and remain
unpublished.

#### Scenario: Release metadata is inspected

- **WHEN** the embedded version, Docker defaults, and `UPSTREAM.md` are read
- **THEN** they identify official v0.1.182 and Plus `custom.001`
- **THEN** the mapping status is `planned`
- **THEN** no tag, Release, or image has been published

### Requirement: Responses Lite preserves Plus security and identity boundaries

Responses Lite HTTP and WebSocket normalization SHALL retain official v0.1.182
compatibility behavior without bypassing Plus security audit, account-scoped
identity, prompt-cache identity, or source-priority enforcement.

#### Scenario: A WebSocket Responses Lite turn is forwarded

- **WHEN** a recognized Responses Lite `response.create` frame is received
- **THEN** its payload is normalized before upstream forwarding
- **THEN** request auditing still occurs before account selection or upstream
      side effects
- **THEN** the final upstream identity follows credential-owner, global, then
      compiled-default precedence

### Requirement: Image prompts are forwarded verbatim

The OAuth image-generation instruction SHALL preserve the user image prompt
without rewriting it, while the Plus outbound identity and multipart-size limit
remain in force.

#### Scenario: An OAuth image request is built

- **WHEN** the request includes a user image prompt
- **THEN** the official instruction requires verbatim forwarding
- **THEN** the request uses the resolved Plus outbound identity
