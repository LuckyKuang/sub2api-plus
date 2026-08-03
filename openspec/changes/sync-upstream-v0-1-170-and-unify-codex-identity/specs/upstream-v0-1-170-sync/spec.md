## ADDED Requirements

### Requirement: Official v0.1.170 behavior and Plus release identity must be preserved together

The repository SHALL map Plus release `v0.1.170+custom.001` to official
`v0.1.170` commit `b22f73e725236790f97d89bf0c3b908a48e591d5`. The embedded
application version MUST be `0.1.170+custom.001` and the OCI tag MUST be
`v0.1.170-custom.001`. The upstream embedded version value MUST NOT replace
the Plus release identity.

#### Scenario: Release metadata is validated

- **WHEN** release metadata, Docker arguments, deployment examples, and
  `UPSTREAM.md` are checked
- **THEN** Git tag, application version, OCI tag, and official baseline MUST
  agree with the declared Plus release

### Requirement: Interrupted stream usage must be recorded exactly once

When an upstream stream fails after reporting billable usage, the gateway SHALL
return the partial result for one usage-recording attempt. A retryable failover
error MUST NOT also return partial usage.

#### Scenario: A stream disconnects after upstream usage

- **WHEN** the gateway observes billable upstream usage and the stream later
  disconnects before terminal completion
- **THEN** the observed usage MUST be recorded once
- **THEN** first-token and first-output metrics MUST retain their existing
  semantic definitions

#### Scenario: A stream is eligible for failover

- **WHEN** a retryable upstream stream error triggers account failover
- **THEN** the failing attempt MUST NOT be separately billed as partial usage

### Requirement: Official Codex compatibility fixes must retain Plus policy boundaries

The gateway SHALL preserve official Codex namespace tools for OAuth Responses
by default, synthesize missing passthrough instructions when required, recover
stale encrypted compaction contexts, and bridge recognized tool-output images.
These behaviors MUST retain Plus authorization, identity, session-isolation,
quota, billing, and first-output semantics.

#### Scenario: An OAuth Responses request contains namespace tools

- **WHEN** a compatible OAuth Responses request is forwarded to the Codex
  backend
- **THEN** namespace tools MUST remain intact unless the explicit compatibility
  flattening mode or compact-specific policy requires transformation

### Requirement: Subscription windows must follow the subscription term

Automatic daily, weekly, and monthly quota windows SHALL be anchored to the
subscription term and SHALL NOT reset after the subscription expiry. Manual
resets remain authoritative.

#### Scenario: A legacy subscription window uses its start-day midnight

- **WHEN** a subscription has an unambiguous legacy midnight initial anchor
- **THEN** the next automatic reset MUST align to its actual subscription
  start time without exceeding the subscription end time
