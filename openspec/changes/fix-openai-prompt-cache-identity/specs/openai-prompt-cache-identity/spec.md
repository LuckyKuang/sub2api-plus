## ADDED Requirements

### Requirement: Responses cache identity must remain stable and aligned

The gateway SHALL resolve one stable, tenant-isolated cache identity for every
cache-eligible Responses request.  It MUST write the same resolved value to the
upstream `prompt_cache_key` body field and canonical `session-id` header.  An
explicit client identity MUST take precedence over a content-derived fallback.

#### Scenario: Official Codex sends a session and body key

- **WHEN** a Codex request supplies `session-id` and `prompt_cache_key`
- **THEN** the gateway MUST derive one stable upstream identity from the
  explicit session-scoped seed
- **THEN** the forwarded body key and canonical session header MUST be equal

#### Scenario: Responses request has no explicit cache identity

- **WHEN** a Responses request has a meaningful first user/input anchor but no
  cache key or supported session header
- **THEN** the gateway MUST derive a stable key from the reusable prompt prefix
- **THEN** later turns that retain that prefix MUST resolve to the same key

#### Scenario: Request contains only a model

- **WHEN** a request has no explicit identity and no meaningful user/input
  anchor
- **THEN** the gateway MUST NOT derive a tenant-wide model-only cache identity

#### Scenario: OAuth fingerprinting supplies a different session

- **WHEN** an OAuth request has a finalized `prompt_cache_key` and the selected
  fingerprint mode generates a different converged session identifier
- **THEN** the final `session-id` and `session_id` headers MUST equal the body
  cache key
- **THEN** fingerprint-owned installation and thread identifiers MUST remain
  present and the thread/client-request headers MUST remain paired

#### Scenario: WebSocket follow-up omits the cache key

- **WHEN** a client-facing Responses WebSocket establishes a cache identity and
  a later `response.create` frame omits `prompt_cache_key`
- **THEN** the later frame MUST inherit the established identity
- **THEN** its forwarded body key MUST remain equal to the upstream handshake
  session identity

#### Scenario: Pooled WebSocket identity changes

- **WHEN** a Responses WebSocket request resolves a cache identity different
  from an idle pooled connection's handshake identity
- **THEN** the gateway MUST NOT reuse that incompatible connection
- **THEN** an in-connection identity change MUST either use a new aligned
  upstream handshake or be rejected before forwarding when transparent
  reconnection cannot preserve continuation state

### Requirement: Current Codex session headers must be supported

The gateway SHALL recognize and forward the bounded current Codex headers
`session-id`, `thread-id`, and `x-client-request-id` while retaining supported
legacy session aliases.  The session identifier MUST participate in sticky
routing; thread/request identifiers MUST NOT replace it as a cache key.

#### Scenario: Current Codex headers are used

- **WHEN** the client sends `session-id`, `thread-id`, and an equal
  `x-client-request-id`
- **THEN** account routing MUST use the session-scoped identifier
- **THEN** the thread and client-request identifiers MUST remain paired on the
  upstream request

### Requirement: Compact must preserve cache controls until finalization

The Compact ingress normalizer SHALL retain `prompt_cache_key` and
`prompt_cache_options`.  After account selection, ChatGPT OAuth MUST remove
cache options, while OpenAI Platform API-key requests MUST retain valid cache
options only for GPT-5.6-family models.

#### Scenario: GPT-5.6 Platform Compact uses cache options

- **WHEN** an API-key Compact request targets a GPT-5.6-family model and
  supplies valid `prompt_cache_options`
- **THEN** the upstream Compact body MUST retain the options and stable key

#### Scenario: OAuth or older model supplies cache options

- **WHEN** the selected account is ChatGPT OAuth or the effective Platform
  model predates GPT-5.6
- **THEN** the gateway MUST remove `prompt_cache_options` before forwarding

### Requirement: Chat-to-Responses conversion must support caching for API keys

An API-key Chat Completions request that is converted to the Responses API
SHALL receive the same stable cache-identity treatment as OAuth conversion.
Raw Chat Completions forwarding MUST NOT receive Responses-only cache fields.

#### Scenario: API-key account uses Responses conversion

- **WHEN** a GPT-5-family Chat Completions request selects an API-key account
  configured to use Responses and has no explicit cache key
- **THEN** the converted upstream body MUST contain a stable
  `prompt_cache_key`

#### Scenario: API-key account uses raw Chat Completions

- **WHEN** the selected account is configured not to use Responses
- **THEN** the raw Chat Completions body MUST NOT gain `prompt_cache_key`

### Requirement: Cache usage aliases must not override canonical details

Usage parsing SHALL read canonical nested cache-read and cache-write fields by
presence, including explicit zero.  Known top-level aliases SHALL be used only
when no canonical nested value is present.

#### Scenario: Nested zero conflicts with a positive alias

- **WHEN** canonical nested `cached_tokens` is zero and a top-level cache-read
  alias is positive
- **THEN** recorded cache-read usage MUST remain zero

#### Scenario: Top-level cache-write aliases conflict

- **WHEN** no canonical nested cache-write field exists, the highest-priority
  top-level cache-write alias is explicitly zero, and a lower-priority alias is
  positive
- **THEN** HTTP and WebSocket usage parsing MUST both record zero cache-write
  tokens

### Requirement: Cache hit rate must use prompt input only

Usage views SHALL calculate cache hit rate as cache-read tokens divided by the
sum of ordinary input, cache-read input, and cache-write input tokens.  Output
tokens MUST NOT be included.

#### Scenario: One quarter of prompt input is read from cache

- **WHEN** ordinary input is 100, cache read is 50, and cache write is 50
- **THEN** the displayed cache hit rate MUST be 25.0 percent
