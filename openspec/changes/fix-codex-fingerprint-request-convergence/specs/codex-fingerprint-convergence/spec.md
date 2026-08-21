## MODIFIED Requirements

### Requirement: HTTP headers and metadata must share one staged fingerprint set

Every eligible OpenAI OAuth Responses turn SHALL resolve the credential owner
and derive at most one fingerprint set from client-original carriers. HTTP and
WebSocket headers plus map/raw `client_metadata` SHALL consume that same set
for the turn. An account change SHALL clear or replace prior request state
before any outbound carrier is built.

#### Scenario: Compatibility bridge creates a Responses turn

- **WHEN** OAuth Chat Completions or Messages is converted to a Responses body
- **THEN** the converted body and final upstream headers SHALL use one staged
  fingerprint set owned by the credential account
- **THEN** no identity from a prior account attempt SHALL remain

#### Scenario: Direct WebSocket sends another turn

- **WHEN** a direct Responses WebSocket accepts a follow-up `response.create`
- **THEN** stable identifiers SHALL retain their configured deterministic scope
- **THEN** per-turn identifiers and timestamp SHALL be generated once for that
  frame and used by every applicable carrier
- **THEN** connection compatibility SHALL compare the final stable handshake
  carriers without omitting client-owned values in `off` or `device` mode
- **THEN** the result SHALL not depend on a credential shadow persisting its own
  fingerprint mode

### Requirement: Fingerprint and prompt-cache identity are composed safely

Fingerprint mutation SHALL run only after OAuth session-policy authorization.
It SHALL NOT change User-Agent, Originator, Version, usage-log session IDs, or
a session-policy decision. The finalized body `prompt_cache_key` SHALL remain
authoritative for final `session-id` and `session_id` headers even when
fingerprint session metadata has a different value.

#### Scenario: Fingerprint session differs from cache session

- **WHEN** `session` or `full` mode produces a fingerprint session and Plus
  resolves a different tenant-isolated body cache key
- **THEN** fingerprint-owned metadata SHALL retain the fingerprint session
- **THEN** final `session-id` and `session_id` SHALL equal the body cache key

### Requirement: Compact protocols use explicit fingerprint policies

Native remote compaction on `/responses` SHALL use the credential owner's full
configured fingerprint mode. The ChatGPT Codex OAuth legacy compact
compatibility path SHALL perform no fingerprint mutation for `off` and
installation-only mutation for `device`, `session`, or `full`. Its session,
cache, thread, and turn namespaces SHALL remain unchanged. This legacy scope
does not describe public API-key `/v1/responses/compact` availability.

#### Scenario: Native compact follows an ordinary turn

- **WHEN** a native `compaction_trigger` turn follows an ordinary turn
- **THEN** the prior stage SHALL be cleared and a new set SHALL be prepared
  according to the credential owner's configured mode
- **THEN** Plus cache identity SHALL remain the final session-header authority

#### Scenario: Legacy compact uses a non-off mode

- **WHEN** a legacy compact request uses `device`, `session`, or `full`
- **THEN** the credential owner's stable installation SHALL be applied
- **THEN** compact session, cache, thread, and turn carriers SHALL not be
  replaced by ordinary Responses values

#### Scenario: Legacy compact uses off

- **WHEN** a legacy compact request uses `off`
- **THEN** no fingerprint-owned body or header carrier SHALL be changed

### Requirement: Existing embedded metadata must not expose split identity

When an active fingerprint mode observes an existing embedded
`x-codex-turn-metadata` carrier, it SHALL overwrite owned fields in a valid
object or rebuild invalid JSON, null, array, and scalar values as a minimal
valid object. Missing embedded carriers SHALL remain missing.

#### Scenario: Embedded metadata is JSON null

- **WHEN** an eligible body or header contains `x-codex-turn-metadata` with the
  JSON value `null`
- **THEN** forwarding SHALL not panic
- **THEN** the rebuilt object SHALL contain the same applicable owned values as
  the flat fingerprint carriers

### Requirement: Fingerprint eligibility follows endpoint semantics

Credential-owning OpenAI OAuth accounts, including PAT and Agent Identity,
SHALL use the same ordinary/native/legacy policy when they carry a Responses
turn. API-key, setup-token, credential-shadow-owned state, token/refresh
operations, and ordinary non-session probes SHALL not create their own
fingerprint stage. A credential shadow carrying a Responses turn SHALL inherit
the credential owner's mode and identifiers.

#### Scenario: Credential shadow carries a Responses turn

- **WHEN** scheduling selects a credential shadow for an eligible turn
- **THEN** preparation SHALL resolve its credential owner
- **THEN** the owner's mode and stable identifiers SHALL be used

#### Scenario: Non-session operation uses OAuth credentials

- **WHEN** models, quota, usage, token exchange, refresh, response retrieve or
  cancel, or another ordinary non-session operation executes
- **THEN** it SHALL still use the selected outbound identity triple
- **THEN** it SHALL not invent installation, session, or thread identifiers
