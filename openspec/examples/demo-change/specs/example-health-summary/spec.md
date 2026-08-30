## ADDED Requirements

### Requirement: Health summary is read-only

The system SHALL expose the current service health as a read-only summary with
an update time and SHALL NOT trigger probes or mutate health state.

#### Scenario: Operator reads the health summary

- **WHEN** an operator requests the health summary
- **THEN** the response contains the current status and update time
- **THEN** no probe runs and no service state changes
