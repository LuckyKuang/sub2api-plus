## ADDED Requirements

### Requirement: Explicit tag publication must authorize automatic release

The release tool SHALL keep tag publication as an explicit maintainer action.
After the exact annotated tag is transferred, the Release workflow SHALL run
its verification matrix and SHALL automatically start `Build and publish` only
after verification succeeds. The release tool MUST NOT approve its own pending
deployment or combine publication, monitoring, verification, and finalization.

#### Scenario: Reviewed release tag is published

- **WHEN** a maintainer explicitly publishes an eligible annotated custom tag
- **THEN** the tag-triggered verification matrix SHALL run
- **THEN** `Build and publish` SHALL start automatically after verification
  succeeds
- **THEN** monitor, verify, and finalize SHALL remain separately resumable

### Requirement: Publication must fail closed on external policy drift

Before transferring a release tag, the release tool SHALL require a `release`
Environment with administrator bypass disabled, no reviewer, timer, or custom
gate, and exactly one deployment policy for `v*+custom.*` tags. It SHALL also
require an active repository Tag ruleset matching those custom tags with no
bypass actors, initial creation allowed, and update and deletion blocked.

#### Scenario: Automated release governance is valid

- **WHEN** the Environment and Tag ruleset satisfy the complete policy
- **THEN** the release tool MAY push only the explicitly named eligible tag

#### Scenario: A manual gate or mutable tag policy is present

- **WHEN** a required reviewer, wait gate, administrator bypass, broader ref
  policy, missing immutability rule, creation restriction, or bypass actor is
  detected
- **THEN** publication MUST stop before tag transfer

### Requirement: Monitoring must expose unexpected waiting gates

Release monitoring SHALL observe the tag-triggered workflow through automatic
publication. A waiting `Build and publish` job SHALL be treated as external
policy drift rather than a normal manual-approval state.

#### Scenario: Environment policy changes after tag transfer

- **WHEN** the Release workflow reaches a waiting Environment gate
- **THEN** monitor MUST fail with the workflow URL and a policy-drift diagnostic
- **THEN** it MUST NOT approve or bypass the deployment
