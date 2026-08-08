## MODIFIED Requirements

### Requirement: Every eligible Codex upstream operation must use one resolved identity

The system SHALL resolve an OpenAI Codex outbound identity from the
credential-owning account custom User-Agent, then the administrator global
setting, then the compiled default. The paired User-Agent, Originator, and
Version MUST derive from one source of truth and every eligible HTTP,
passthrough, WebSocket, probe, model, refresh, and retry operation MUST reuse
the resolved identity. Official client-version synchronization and identity
normalization SHALL update that source without introducing another identity
resolver.

#### Scenario: An automatic client-version refresh succeeds

- **WHEN** the configured synchronization interval obtains a valid official
  Codex client version
- **THEN** subsequent eligible outbound requests MUST use a User-Agent and
  Version derived from that version
- **THEN** the Originator MUST remain paired with the resulting User-Agent

#### Scenario: An upstream Codex request is load-shed

- **WHEN** a Codex request receives a retryable overload or slow-down signal
- **THEN** the gateway MUST apply bounded same-account retry before eligible
  account failover
- **THEN** no retry path may substitute an unpaired or stale outbound identity
