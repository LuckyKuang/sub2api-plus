## ADDED Requirements

### Requirement: Security audit stays on the canonical extraction contract

Content Moderation and Prompt Audit SHALL consume the same canonical protocol
extraction contract. Prompt Audit SHALL select conversation text and SHALL NOT
treat tool-schema dumps as jailbreak text. Unknown item types, unknown
Responses/Live frames, unknown sibling fields, valid-JSON unrecognized
structures, and other incomplete or unextractable content SHALL pass through
without an audit-derived block.

#### Scenario: A Codex request carries a large tool schema and short user text

- **WHEN** Prompt Audit evaluates a request whose tools schema is large and
  whose conversation text is benign
- **THEN** the tools schema SHALL NOT be scored as user jailbreak text
- **THEN** the request SHALL not be blocked by an extraction or schema-size
  artifact

### Requirement: Audit runs before side effects

Every accepted HTTP/WS request or turn SHALL enter security audit after
auth/basic validation and before account selection, billing, concurrency
acquisition, upstream writes, or other side effects.

#### Scenario: An audited request is about to select an upstream account

- **WHEN** the request has passed authentication and basic validation
- **THEN** security audit SHALL complete before account selection, billing,
  concurrency acquisition, or upstream writes

### Requirement: Plus-only operator controls remain

The admin user support view SHALL remain read-only. The global IP access
control switch SHALL remain present and default off. Usage TPS based on
`last_token_ms` SHALL remain available beside official usage aggregation.

#### Scenario: An administrator opens user support and IP settings

- **WHEN** the admin UI and configuration defaults are inspected
- **THEN** the user support view SHALL be readable and not writable
- **THEN** `global_ip_access_control_enabled` SHALL default to disabled
