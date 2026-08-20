## ADDED Requirements

### Requirement: Active OpenAI sessions must use one cross-endpoint account route

The gateway SHALL derive the same sticky route for Responses and Alpha Search
when they carry the same stable client session. Direct stable session headers
MUST take precedence over `X-Codex-Turn-Metadata.session_id`, and per-request
turn identifiers MUST NOT be used as session routes.

#### Scenario: Responses and Alpha Search share turn metadata

- **WHEN** both endpoints carry the same non-empty turn-metadata `session_id`
  and different request or search identifiers
- **THEN** they MUST derive the same session route
- **THEN** under the default hard-affinity mode, a priority change MUST NOT move
  only one endpoint to another account

#### Scenario: Direct session header conflicts with metadata

- **WHEN** a request supplies both a direct `session-id` and a different
  turn-metadata `session_id`
- **THEN** the direct header MUST determine the route

#### Scenario: Metadata contains only a turn identifier

- **WHEN** turn metadata is malformed or contains no stable string `session_id`
- **THEN** it MUST NOT replace the endpoint's existing fallback behavior

#### Scenario: Usage is correlated from turn metadata

- **WHEN** a successful OpenAI request has no direct session header and carries
  a valid turn-metadata `session_id`
- **THEN** its usage record and cyber-session key MUST use that sanitized
  stable session value
- **THEN** a search ID, request ID, content hash, or `turn_id` MUST NOT be
  persisted as the client session ID

### Requirement: Current group membership must gate response continuation

Every account resolved from a `previous_response_id` SHALL be revalidated
against the current persisted scheduling group before forwarding. A mismatch
MUST invalidate the current group's local response binding and MUST NOT delete
OAuth global ownership markers.

#### Scenario: API-key account is removed from a group

- **WHEN** a response binding points to an API-key account that no longer
  belongs to the requesting group
- **THEN** the gateway MUST NOT reuse that account
- **THEN** normal scheduling MAY select another account in the group

#### Scenario: Ordinary OAuth account is removed from a group

- **WHEN** session sharing is disabled and the bound OAuth account no longer
  belongs to the requesting group
- **THEN** the gateway MUST apply the same invalidation as for an API-key
  account

#### Scenario: Sharing-enabled OAuth policy changes

- **WHEN** the requesting group is removed from the bound OAuth account's
  validated sharing policy
- **THEN** the local group binding MUST be invalidated
- **THEN** the global response owner and scope markers MUST remain intact

### Requirement: Existing WebSocket connections must revalidate each turn

Before forwarding each Responses WebSocket turn, every ingress mode SHALL
apply billing eligibility once for that turn and SHALL reload the connection's
selected account from persistent storage. The refreshed account SHALL pass
current group, status, schedulability, quota, client-model, endpoint-capability,
transport, parent-health, runtime-block, threshold, and proxy-quarantine gates.
A mismatch or inability to refresh the account MUST fail closed before that
turn reaches upstream.

#### Scenario: Account leaves the group while a WebSocket remains connected

- **WHEN** an API-key, ordinary OAuth, or sharing-enabled OAuth account was
  valid when the connection opened but is removed from the requesting group
  before a later turn
- **THEN** the gateway MUST NOT forward that later turn on the existing
  upstream connection
- **THEN** it MUST close the client connection with a retryable status so a
  reconnect can run normal scheduling

#### Scenario: Passthrough connection starts another turn

- **WHEN** a passthrough WebSocket receives a subsequent `response.create`
- **THEN** it MUST run the same pre-turn group, profit, pricing, and concurrency
  lifecycle as ctx-pool and HTTP-bridge ingress before writing upstream

#### Scenario: Billing eligibility changes between turns

- **WHEN** the first turn passed billing but a later turn no longer has an
  eligible balance, subscription, quota, API-key limit, or RPM decision
- **THEN** the later turn MUST NOT reach upstream
- **THEN** the first turn and each attempted follow-up MUST be checked exactly
  once rather than double-counting the first turn

#### Scenario: Current account becomes otherwise ineligible

- **WHEN** an established connection's account is disabled, unschedulable,
  quota-paused, incompatible with the current client model/capability/transport,
  parent-unhealthy, runtime-blocked, threshold-blocked, or proxy-quarantined
- **THEN** the gateway MUST close before forwarding the next turn and require a
  reconnect for normal account selection

#### Scenario: Follow-up turn introduces image generation

- **WHEN** a later `response.create` introduces explicit image-generation
  intent
- **THEN** every ingress mode MUST recheck the group's image permission and the
  selected account's Responses capability before writing upstream

#### Scenario: Channel mapping differs from account whitelist identity

- **WHEN** the current client model is allowed by the selected account and a
  channel maps it to a different upstream model
- **THEN** durable eligibility MUST validate the client model as HTTP selection
  does
- **THEN** the mapped model MUST still be used for upstream forwarding and
  mapping-aware usage attribution

### Requirement: Safe failover must migrate the whole session route

An established replacement session route SHALL supersede an older response
binding only when a Responses WebSocket request can reconstruct its
continuation without that response ID. The WebSocket handler MUST remove the
stale ID before forwarding to the replacement account.

#### Scenario: Alpha Search failure moves the shared session

- **WHEN** Alpha Search fails over from account A to account B and updates the
  shared session route
- **THEN** a later movable Responses request in that session MUST select B
- **THEN** it MUST NOT send account A's `previous_response_id` to B

#### Scenario: Tool continuation cannot be reconstructed

- **WHEN** a Responses request has tool outputs whose call context is not fully
  present in the input
- **THEN** the gateway MUST NOT remove `previous_response_id`
- **THEN** OAuth response ownership validation MUST remain fail closed

#### Scenario: Movable continuation temporarily escapes a degraded sticky account

- **WHEN** sticky escape is enabled, the canonical session account is degraded,
  and a movable Responses request also carries that account's response ID
- **THEN** the old response binding MUST NOT cancel the session sticky escape
- **THEN** the temporary replacement selection MUST preserve the canonical
  sticky binding and MUST use the stable session as its cross-endpoint seed

#### Scenario: Sharing-enabled OAuth replacement is authorized

- **WHEN** an old OAuth response route is invalid, another sharing-enabled
  OAuth account is authorized for the group, and the request is movable
- **THEN** the replacement account MAY be selected
- **THEN** ownership validation of the removed response ID MUST NOT reject the
  replacement before forwarding
