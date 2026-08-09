## ADDED Requirements

### Requirement: Official v0.1.173 and Plus release behavior must coexist

The repository SHALL integrate official v0.1.173 commit
29009f0b2ea14edf3b11ae2564fb617ff91a03b4 while retaining intentional Plus
identity, quota, migration, deployment, and distribution behavior. Prepared
release metadata MUST identify v0.1.173+custom.001 and MUST NOT imply that a tag,
Release, or image has already been published.

#### Scenario: Release preparation is validated

- **WHEN** embedded version, Docker arguments, deployment examples, release
  notes, and UPSTREAM.md are checked
- **THEN** they MUST agree on v0.1.173+custom.001 and official v0.1.173
- **THEN** the upstream mapping status MUST remain non-published until the
  separate publication workflow succeeds

### Requirement: Imported migrations must preserve published Plus history

Official v0.1.173 migrations SHALL use unique, increasing Plus prefixes after
the published local maximum. Published migration files MUST remain immutable,
and unpublished draft checksums MUST NOT be accepted as historical versions.

#### Scenario: Upstream migration numbers collide with Plus history

- **WHEN** an official migration prefix is already allocated in a published
  Plus release
- **THEN** its resolved SQL MUST use a new Plus-owned prefix
- **THEN** no compatibility checksum may be added solely for a pre-release
  draft of the newly prefixed migration

### Requirement: Frontend dependency locking must preserve Plus ownership

The merged frontend lockfile SHALL remain synchronized with the retained Plus
dependency declarations and security overrides. An upstream lock graph that
predates those declarations MUST NOT replace it solely because of merge
conflict resolution.

#### Scenario: The official release does not change frontend dependencies

- **WHEN** the official release changes neither `frontend/package.json` nor
  `frontend/pnpm-lock.yaml`, while the Plus parent has a synchronized newer graph
- **THEN** the merge MUST retain the Plus graph without unrelated re-resolution
- **THEN** `pnpm install --frozen-lockfile` MUST succeed

### Requirement: Gemini forwarding must preserve pool and image accounting semantics

Gemini pool-mode accounts without custom error-code handling SHALL remain
schedulable after a 429 so bounded retry and failover can recover. Gemini image
usage SHALL prefer observed inline image outputs and fall back to recognition of
the requested or mapped model only when no output image can be observed.

#### Scenario: A pool-mode account receives a 429

- **WHEN** the account uses pool mode and has no custom error-code policy
- **THEN** account-level rate-limit state MUST NOT be written for that 429
- **THEN** retry and failover policy MUST remain responsible for recovery

#### Scenario: Error policy has already skipped or temporarily unscheduled an account

- **WHEN** a Gemini compatibility path receives `ErrorPolicySkipped` or
  `ErrorPolicyTempUnscheduled`
- **THEN** it MUST NOT invoke the default account-state error handler
- **THEN** a skipped pool-mode authentication response MUST remain available to
  the bounded failover policy without being locally disabled

#### Scenario: A custom model alias returns inline images

- **WHEN** the requested and mapped names are not recognized image-model names
  but the upstream response contains valid inline image parts
- **THEN** the usage result MUST count those observed images
- **THEN** a failed forwarding attempt MUST NOT leak its count into a later
  failover attempt

### Requirement: Antigravity model observation must retain envelope metadata

Antigravity streaming response-model observation SHALL inspect the original SSE
event payload before response-envelope unwrapping.

#### Scenario: Model metadata exists only in the event envelope

- **WHEN** an SSE event contains response-model metadata outside its inner
  response payload
- **THEN** the observer MUST capture that metadata from the original event

### Requirement: OAuth session sharing must cover every Responses transport

Ordinary HTTP, passthrough HTTP, and WebSocket Responses builders SHALL resolve
OAuth session and conversation identifiers through the
same Plus account-sharing policy. The outbound Codex identity source precedence
MUST remain unchanged.

#### Scenario: Authorized groups use different API keys

- **WHEN** two allowed API-key groups belonging to the same user send the same
  raw session identifier through any supported Responses transport
- **THEN** every transport MUST derive the same account-scoped upstream session
  identifier

#### Scenario: An unauthorized group supplies a session identifier

- **WHEN** the selected OAuth account has session sharing enabled but the
  request group is not authorized by its policy
- **THEN** the request builder MUST fail closed before forwarding upstream

### Requirement: Stream errors must preserve observed accounting results

OpenAI and Grok Responses streaming wrappers SHALL return the partial forwarding
result alongside a terminal stream error whenever the shared stream parser has
observed result state.

#### Scenario: The client disconnects before terminal output is written

- **WHEN** upstream output and terminal usage have been observed but downstream
  writing fails
- **THEN** the returned result MUST retain usage, request and response identity,
  first-token and first-output timing, output kind, and client-disconnect state
- **THEN** the stream error MUST still be returned to mark the request
  incomplete

### Requirement: Grok request construction and token aggregation must be complete

Anthropic Messages traffic routed to Grok SHALL honor the configured global
Grok base-URL mode on both the initial request and encrypted-content retry.
OpenAI usage aggregation SHALL preserve audio output tokens in addition to all
existing token classes.

#### Scenario: A Grok Messages request retries invalid encrypted content

- **WHEN** the first request is rejected for invalid encrypted reasoning and an
  account has no explicit base URL
- **THEN** both attempts MUST use the globally configured Grok endpoint

#### Scenario: Multiple usage fragments contain audio output

- **WHEN** OpenAI-compatible response fragments are aggregated
- **THEN** audio output tokens MUST be added without dropping text, cache,
  image-input, or image-output tokens
