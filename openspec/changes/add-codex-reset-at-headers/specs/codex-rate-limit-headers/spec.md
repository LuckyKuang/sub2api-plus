## ADDED Requirements

### Requirement: Codex responses expose absolute reset timestamps

The gateway SHALL emit `X-Codex-Primary-Reset-At` and
`X-Codex-Secondary-Reset-At` as Unix-second timestamps whenever it emits a
local Primary or Secondary Codex quota window. It SHALL retain the matching
`Reset-After-Seconds` headers.

#### Scenario: Local subscription quota response

- **WHEN** local Codex subscription quota is enabled for an OpenAI subscription group
- **THEN** each active local quota window includes consumed percentage, window
  minutes, absolute reset time, and relative reset seconds

### Requirement: WebSocket upgrade headers use pre-upgrade local data

The gateway SHALL apply an enabled local Codex subscription quota view before
committing a client-facing WebSocket `101` response. When the local view is
disabled, the gateway SHALL leave Codex quota headers uninjected because an
upstream handshake occurs only after the client upgrade is committed.

#### Scenario: Client WebSocket upgrade

- **WHEN** a client upgrades a Codex Responses request to WebSocket
- **THEN** its `101` response includes both local reset forms when the local
  view is enabled, and includes no locally injected Codex quota headers when
  the local view is disabled

### Requirement: Local quota responses do not mix sources

The gateway SHALL remove both absolute and relative upstream reset headers
before it writes local Codex quota headers.

#### Scenario: Upstream also supplied quota headers

- **WHEN** an eligible upstream response includes Codex quota fields and local
  subscription quota is enabled
- **THEN** the client receives only local values for every emitted quota field

### Requirement: Upstream absolute reset timestamps are understood

The gateway SHALL parse a valid `X-Codex-*-Reset-At` as the authoritative reset
time, convert it to the existing relative snapshot fields using one captured
request time, and use a valid legacy relative field only when the absolute
value is absent or invalid.

#### Scenario: Conflicting reset headers

- **WHEN** an upstream response includes valid absolute and relative reset
  values that differ
- **THEN** internal snapshot and 429 scheduling use the absolute value

#### Scenario: Expired absolute reset timestamp

- **WHEN** an upstream absolute reset timestamp is not after the captured
  request time
- **THEN** the internal relative reset value is zero seconds

#### Scenario: Absolute timestamp exceeds the supported duration

- **WHEN** an upstream absolute reset timestamp is parseable but its relative
  offset cannot be represented safely as a Go duration
- **THEN** the gateway treats the absolute value as invalid and uses a valid
  legacy relative value when available
