## MODIFIED Requirements

### Requirement: Every eligible Codex upstream operation must use one resolved identity

The system SHALL resolve an OpenAI Codex outbound identity in this immutable
source order: a valid custom User-Agent from the credential-owning account,
then a valid administrator global User-Agent, then the compiled default as the
final fallback. Empty or invalid candidates MUST fall through only to the next
source. A valid higher-priority source MUST NOT be replaced by version
synchronization, inbound headers, generic header overrides, request
classification, retries, probes, or any lower-priority source.

After selecting the source, the system MAY rebuild only the selected
User-Agent's leading and official trailing version declarations from the
effective version. Rebuilding MUST preserve the selected source, client family,
paired Originator, OS, architecture, and terminal fingerprint. User-Agent,
Originator, and Version MUST form one coherent triple, and every eligible HTTP,
passthrough, WebSocket, image, probe, model, refresh, quota, PAT, and retry
operation MUST reuse the resolved identity.

#### Scenario: A credential-owning account has a valid custom User-Agent

- **WHEN** both the credential-owning account and the global setting contain
  valid Codex User-Agents
- **THEN** the account User-Agent MUST be the selected source
- **THEN** neither the global nor compiled default identity may replace its
  client family or platform fingerprint

#### Scenario: The account candidate is empty or invalid

- **WHEN** the credential-owning account has no valid custom User-Agent
- **THEN** the valid global User-Agent MUST be the selected source
- **THEN** the compiled default MUST NOT replace the valid global identity

#### Scenario: No configured candidate is valid

- **WHEN** neither the credential-owning account nor the global setting contains
  a valid Codex User-Agent and no supported explicit or newer stable automatic
  version is effective
- **THEN** the system MUST use User-Agent
  `codex-tui/0.147.0 (Ubuntu 24.04; x86_64) xterm-256color`, Originator
  `codex-tui`, and Version `0.147.0` as one complete fallback triple

#### Scenario: Automatic synchronization contains a stale version

- **WHEN** no explicit administrator version is configured and the persisted
  automatic version is invalid, prerelease, or older than the compiled version
- **THEN** the compiled version MUST be used as the effective version
- **THEN** applying that version MUST NOT change the already selected account,
  global, or default identity source

#### Scenario: Automatic synchronization advances the version

- **WHEN** a stable official version not older than the compiled version becomes
  effective
- **THEN** only the selected User-Agent's version declarations and Version
  header MAY change
- **THEN** its source, client family, paired Originator, OS, architecture, and
  terminal fingerprint MUST remain unchanged

#### Scenario: A Spark shadow or nested operation uses an account identity

- **WHEN** a Spark shadow, refresh, retry, probe, or nested request operates on
  an already selected credential-owning account
- **THEN** it MUST reuse that credential owner's resolved identity
- **THEN** it MUST NOT restart resolution from the global or default source

#### Scenario: An upstream Codex request is load-shed

- **WHEN** a Codex request receives a retryable overload or slow-down signal
- **THEN** the gateway MUST apply bounded same-account retry before eligible
  account failover
- **THEN** no retry path may substitute an unpaired, stale, or lower-priority
  outbound identity

#### Scenario: An untrusted input conflicts with the selected identity

- **WHEN** an inbound request, generic account header override, or request
  classification flag supplies conflicting identity headers
- **THEN** the final upstream request MUST retain the trusted three-level
  resolution result
