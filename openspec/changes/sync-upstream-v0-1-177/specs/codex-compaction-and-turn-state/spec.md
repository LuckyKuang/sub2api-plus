## ADDED Requirements

### Requirement: Native and legacy compaction must remain distinct

Native remote compaction v2 SHALL remain a Responses request on `/responses`
and SHALL be recognized only for streaming input containing a
`compaction_trigger`. Legacy compaction SHALL remain the endpoint family rooted
at `/responses/compact`, including existing structurally safe forwarded
subpaths, with its compact model mapping and unary response handling.

#### Scenario: Native compaction is scheduled

- **WHEN** bare `/responses` has `stream: true` and a compaction trigger
- **THEN** the upstream path SHALL remain `/responses`
- **THEN** Responses capability and Plus text-profit admission SHALL apply
- **THEN** the session beta features SHALL contain `remote_compaction_v2`

#### Scenario: Legacy compact is scheduled

- **WHEN** the suffix is `/compact` or an existing structurally safe
  `/compact/...` forwarded subpath
- **THEN** legacy compact capability and model mapping SHALL apply
- **THEN** unary compact response handling SHALL apply

#### Scenario: A structurally unsafe path resembles a supported route

- **WHEN** the path has leading/trailing whitespace or a structurally unsafe
  suffix
- **THEN** it SHALL not be normalized into a supported Responses route
- **THEN** it SHALL be rejected by the suffix allowlist guard

### Requirement: Turn-state echo must be credential-owner scoped

`x-codex-turn-state` provenance SHALL be associated with the
credential-owning account. A known foreign owner's state SHALL be stripped,
while owner/shadow transitions for the same credential SHALL be permitted.

#### Scenario: A response commits turn state

- **WHEN** the response header is actually committed to the client
- **THEN** provenance SHALL be recorded for the credential owner
- **THEN** a later request using that credential MAY echo the value

#### Scenario: An upstream attempt fails before committing a response

- **WHEN** an attempt receives or parses state but is abandoned, fails, or retries
- **THEN** it SHALL NOT record provenance
- **THEN** that attempt SHALL NOT authorize a later echo

#### Scenario: A known state belongs to another credential owner

- **WHEN** account B sends a state recorded for account A
- **THEN** the outbound request SHALL strip the state
