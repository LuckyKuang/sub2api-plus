## ADDED Requirements

### Requirement: Late OAuth path construction cannot replace the resolved identity

Messages, native Alpha Search, Alpha Search Responses fallback, and OAuth model
manifest operations SHALL apply the credential-owner-aware resolved identity
after endpoint-specific header staging and generic overrides. User-Agent,
Originator, and Version SHALL be written together from that resolution.
Inbound identity headers, gateway.force_codex_cli, endpoint compatibility
helpers, and lower-priority configured sources SHALL NOT replace a valid
higher-priority source.

#### Scenario: Messages restores required compatibility headers

- **WHEN** the Messages bridge restores its required Responses beta header
- **THEN** the final identity SHALL still use the valid credential-owner source
- **THEN** User-Agent, Originator, and Version SHALL remain coherent

#### Scenario: Alpha Search receives conflicting identity inputs

- **WHEN** an Alpha Search request carries inbound identity headers or force
  mode is enabled
- **THEN** those values SHALL NOT participate in outbound identity resolution
- **THEN** native and Responses-fallback requests SHALL use the immutable
  credential-owner, global, compiled-default source order

#### Scenario: OAuth model manifest uses a synchronized version

- **WHEN** an effective synchronized or administrator version is selected
- **THEN** the model manifest client_version query SHALL equal the final
  Version header
- **THEN** the User-Agent version declaration and paired Originator SHALL
  derive from the same resolved identity

#### Scenario: A credential shadow synchronizes models

- **WHEN** an OAuth credential shadow triggers model synchronization
- **THEN** authentication and outbound identity SHALL resolve through its
  credential-owning parent
- **THEN** the shadow SHALL NOT fall through independently to global or default

#### Scenario: Agent Identity task recovery overlaps a settings update

- **WHEN** task registration begins with one resolved identity and the
  administrator updates the global identity or version before registration
  completes
- **THEN** task registration and the immediately retried upstream request SHALL
  use the same original identity snapshot
- **THEN** the updated setting SHALL apply only to a subsequently resolved
  request
