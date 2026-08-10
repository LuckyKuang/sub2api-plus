## ADDED Requirements

### Requirement: Responses input identifiers must retain their valid type prefix

The OpenAI Responses request sanitizer SHALL preserve a non-empty item `id`
only when it matches the documented prefix for that item type.  It MUST
preserve a `custom_tool_call` identifier beginning with `ctc`, and it MUST
remove a mismatched identifier before forwarding.

#### Scenario: A replayed custom tool call has a ctc identifier

- **WHEN** a Responses input item has type `custom_tool_call` and an id that
  begins with `ctc`
- **THEN** the upstream request MUST retain that id and its call id

#### Scenario: A replayed custom tool call has an invalid identifier

- **WHEN** a `custom_tool_call` input id does not begin with `ctc`
- **THEN** the sanitizer MUST remove the input id before forwarding

### Requirement: Retry-buffer overflow must not retry or degrade account state

The exact proxy retry-buffer-limit response with upstream status `507` SHALL
be represented as a request-scoped client `413`.  It MUST NOT retry another
account or record an account-level health, cooldown, disablement, or capacity
failure.

#### Scenario: A retry buffer exceeds its safe limit

- **WHEN** forwarding receives the configured retry-buffer-limit `507` error
- **THEN** the client MUST receive the stable `413` request-size response
- **THEN** the selected account MUST remain eligible absent another error

### Requirement: OAuth policy denial must not mask another selection outcome

The scheduler SHALL return a session-policy denial only when policy is the
actual selection blocker.  Disabled, temporarily unavailable, capacity-limited,
or otherwise authorized candidates MUST preserve their own selection outcome.

#### Scenario: An authorized candidate is temporarily unavailable

- **WHEN** a session-sharing request has an authorized matching account that
  cannot currently be selected for a non-policy reason
- **THEN** the request MUST NOT be rewritten as an OAuth policy denial

### Requirement: Operations errors must distinguish routing capacity from business exclusions

The system SHALL store sanitized structured routing diagnostics and an
independent routing-capacity marker for routing failures.  Existing
business-limit data MUST remain available for SLA compatibility.

#### Scenario: A local scheduler has no eligible account

- **WHEN** account selection fails without an upstream response
- **THEN** the Ops error record MUST identify it as routing capacity and store
  sanitized candidate/filter diagnostics
- **THEN** the default Error view MUST include the record

### Requirement: Administration last-24-hours must be a rolling backend range

The administration error query SHALL send `time_range=24h` for the selected
Last 24 Hours preset.  Custom date ranges SHALL continue to send explicit
RFC3339 start and end bounds.

#### Scenario: An administrator selects Last 24 Hours

- **WHEN** the error list is loaded with the Last 24 Hours preset
- **THEN** the API query MUST use `time_range=24h`
- **THEN** it MUST NOT substitute local calendar-day boundaries

### Requirement: Outbound identity diagnostics

When an OpenAI request error is recorded, the system MUST retain the existing
account, global, and compiled-default identity precedence and outbound
fingerprint. It MAY store only the selected safe outbound-identity source.

#### Scenario: A credential-owning account supplies a valid user agent

- **WHEN** an error is recorded for that account
- **THEN** diagnostics MAY identify the account source
- **THEN** the forwarded User-Agent, Originator, and Version MUST still come
  from that account identity
