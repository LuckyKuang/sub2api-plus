## ADDED Requirements

### Requirement: Threshold-reaching failures must commit atomically

The system SHALL persist each credential failure in the active statistics
window. When the resulting count reaches the configured threshold, the same
transaction MUST persist the increment and an effective automatic block unless
an allow or existing block rule suppresses creation of a new rule.

#### Scenario: Second failure reaches a threshold of two

- **WHEN** the same normalized IP records two credential failures in one window
- **THEN** the persisted failure count MUST advance from one to two
- **THEN** the second transaction MUST create an active `auto_block`
- **THEN** the automatic rule expiry MUST use `login_failure_block_minutes`

### Requirement: Login-failure windows must support the operational retention range

The system SHALL accept a login-failure statistics window from 1 through
525,600 minutes inclusive. Request validation, persisted-setting parsing, and
the administration input MUST use the same bound.

#### Scenario: Administrator configures a window longer than one day

- **WHEN** an administrator saves `login_failure_window_minutes=10080`
- **THEN** the setting MUST be accepted and persisted
- **THEN** failure counting and cleanup MUST use the seven-day window

#### Scenario: Administrator exceeds the supported range

- **WHEN** an administrator saves `login_failure_window_minutes=525601`
- **THEN** the API MUST reject the request as invalid

### Requirement: Failure-state refresh must be administrator controlled

The failure-state table SHALL load on initial navigation and in response to
explicit table navigation or management actions. It MUST NOT poll on a timer or
refresh merely because document visibility changes.

#### Scenario: Administrator leaves the page open

- **WHEN** the failure-state page remains visible without user interaction
- **THEN** elapsed time MUST NOT trigger another failure-state request

#### Scenario: Administrator requests current state

- **WHEN** the administrator selects Refresh
- **THEN** the table MUST request and display the current failure states

### Requirement: Failure-state quick blocks must be permanent manual rules

The failure-state quick-block action SHALL return an exact-IP active
`manual_block` whose `expires_at` is null. It MUST preserve the failure state
and MUST NOT derive its lifetime from `login_failure_block_minutes`.

#### Scenario: Unblocked failure source is manually blocked

- **WHEN** an administrator confirms a quick block for an eligible failure-state row
- **THEN** the system MUST create an exact-IP active `manual_block`
- **THEN** its `expires_at` MUST be null
- **THEN** the existing failure counter MUST remain unchanged

#### Scenario: Exact temporary manual rule already exists

- **WHEN** a quick-block request finds an active exact-IP `manual_block` with an expiry
- **THEN** the existing rule MUST be upgraded so its `expires_at` is null
- **THEN** the action MUST NOT create a second active exact-IP manual rule

#### Scenario: Automatic block wins a concurrent threshold race

- **WHEN** an exact-IP automatic threshold block commits before the serialized quick-block action
- **THEN** the quick-block action MUST still create or upgrade an exact permanent `manual_block`
- **THEN** the exact-IP automatic rule MUST be retained as released history and MUST NOT remain active
- **THEN** the returned rule MUST be that permanent manual rule
- **THEN** `already_blocked` MAY be true to report the pre-existing effective block

#### Scenario: Permanent quick block is later released

- **WHEN** an administrator releases the permanent manual rule created after an automatic-block race
- **THEN** the superseded exact-IP automatic rule MUST NOT continue blocking the IP
- **THEN** independently covering CIDR rules MUST remain unchanged
