## ADDED Requirements

### Requirement: Official v0.1.183 and Plus release behavior coexist

The repository SHALL integrate official v0.1.183 commit
`e8cb019fabf8b55199436229044cbf9aa7a82564` while retaining intentional Plus
identity precedence, audit ordering, fingerprint, deployment, and distribution
behavior. Prepared metadata SHALL identify `v0.1.183+custom.001` and remain
unpublished.

#### Scenario: Release metadata is inspected

- **WHEN** the embedded version, Docker defaults, and `UPSTREAM.md` are read
- **THEN** they identify official v0.1.183 and Plus `custom.001`
- **THEN** the mapping status is `planned`
- **THEN** no tag, Release, or image has been published

### Requirement: Sticky spillover does not rewrite Plus session bindings

OpenAI load-aware selection SHALL treat a full sticky wait queue as a
one-request capacity spillover and SHALL NOT rewrite the durable Plus sticky
binding, including the OAuth cross-group bind helper.

#### Scenario: Sticky account wait queue is full

- **WHEN** Layer 1 sticky acquisition fails because the wait queue is full
- **THEN** Layer 2 may select another account for this request
- **THEN** the durable session binding is left unchanged

### Requirement: Codex session-id remains the canonical sticky header

Sticky routing and WebSocket session resolution SHALL prefer
`codexSessionIDHeader` (`session-id`) over `session_id` while keeping Plus
module identity and source names.

#### Scenario: Both session headers are present

- **WHEN** a request includes `session-id` and `session_id`
- **THEN** sticky hashing and WebSocket session resolution use `session-id`
