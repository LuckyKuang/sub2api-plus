## ADDED Requirements

### Requirement: Upstream v0.1.169 security baseline and Plus release identity must remain exact

The repository SHALL map Plus Git tag `v0.1.169+custom.001` to official
`v0.1.169` commit `26d894ef4f50645a4bf1030e378ac892f17d0223`. The embedded
application version MUST be `0.1.169+custom.001`, and the OCI tag MUST be
`v0.1.169-custom.001` without changing the Git/GitHub tag semantics.

#### Scenario: Release metadata is checked

- **WHEN** release metadata, Docker build arguments, deployment examples, and
  `UPSTREAM.md` are validated
- **THEN** the upstream mapping, application version, Git tag, and OCI tag
  MUST agree with the declared identities
- **THEN** upstream's embedded version value MUST NOT overwrite the Plus
  release identity

### Requirement: Client-controlled upstream path segments must be rejected before side effects

The gateway MUST validate every client-controlled segment appended to an
upstream Responses, Codex alias, or Gemini model-action path against a closed,
protocol-specific set before account selection, quota or billing mutation, or
upstream dispatch. Separators, empty segments, dot-segment semantics, encoded
path semantics, and characters outside the allowed identifier grammar MUST be
rejected.

#### Scenario: A malicious response subpath is supplied

- **WHEN** a request includes a traversal-like, encoded-separator, or otherwise
  invalid Responses subpath
- **THEN** the gateway MUST return a client input error before selecting an
  account, reserving quota, creating billing state, or dispatching upstream
- **THEN** pooled upstream credentials MUST NOT be usable to address an
  arbitrary upstream endpoint

#### Scenario: A path contains encoded whitespace

- **WHEN** a Responses suffix or Gemini model/action contains leading,
  trailing, encoded, or literal whitespace
- **THEN** the gateway MUST reject it rather than trim or otherwise normalize
  it into a valid upstream path

#### Scenario: A supported path action is supplied

- **WHEN** a client sends a documented compatible action such as
  `responses/compact` or a supported cancel action
- **THEN** the closed-set validation MUST accept the action
- **THEN** existing compatible routing behavior MUST remain available

### Requirement: Proxy stream circuit fallback must not relax Plus authorization

OpenAI proxy stream circuit isolation SHALL be a scheduling preference only.
When all otherwise eligible candidates are isolated, the scheduler MAY retry once
without that isolation preference. OAuth shared-session group authorization and
`previous_response_id` cross-group authorization MUST remain mandatory in both
the initial selection and any fallback selection.

#### Scenario: All candidates are circuit-isolated

- **WHEN** an OpenAI request has otherwise eligible accounts but every candidate
  is isolated by the active proxy stream circuit
- **THEN** the scheduler MAY retry once while bypassing only circuit isolation
- **THEN** model, capability, quota, health, temporary-unschedulable, channel,
  and OAuth authorization filters MUST still apply

#### Scenario: The request crosses an OAuth shared session group

- **WHEN** a request references a `previous_response_id` owned by a different
  OAuth shared session group and circuit fallback is otherwise eligible
- **THEN** the scheduler MUST reject the request with the existing authorization
  error
- **THEN** it MUST NOT retry with a cross-group account

### Requirement: Release artifacts and deployment templates must preserve offline runtime safety

Container images and direct-download archives SHALL both include
`resources/model-pricing/model_prices_and_context_window.json` at the relative
runtime path used by the pricing fallback. Every supported Compose variant SHALL
enable `no-new-privileges:true` for the application and pass the trusted-proxy,
forwarded-IP ACL, emergency-allowlist, and proxy-circuit configuration variables.

#### Scenario: Remote pricing refresh is unavailable in an extracted archive

- **WHEN** an application starts from a release archive and its remote pricing
  refresh cannot succeed
- **THEN** the pricing service MUST be able to load the bundled fallback file
  from the documented relative resource path
- **THEN** it MUST NOT require an image-only resource to start

#### Scenario: An operator uses a Compose template

- **WHEN** an operator starts any maintained Compose template
- **THEN** the application container MUST receive the documented security
  variables and MUST have `no-new-privileges:true`
- **THEN** template defaults and documentation MUST state that trusted proxies
  are only direct proxy or container CIDRs and that forwarded API-key ACL IP
  trust is disabled unless deliberately enabled

### Requirement: Remote pricing updates must be Release-bound and integrity validated

The service SHALL treat remote pricing as financial input. With no deployment
key configuration, it MUST discover the latest maintainer-controlled GitHub
Release manifest and accept a remote update only when that manifest binds an
immutable HTTPS data URL, a SHA-256 digest, and a strictly newer release
version. The data URL MUST be the versioned
`/releases/download/<version>/model-pricing.json` asset and MUST NOT be a
mutable `/latest/` discovery endpoint. The service MUST validate the configured
URL and every redirect against the dedicated pricing host allowlist even when
the global URL-allowlist compatibility switch is disabled.

#### Scenario: A validated latest Release has new pricing

- **WHEN** a valid latest-Release manifest declares a newer version and its
  pricing JSON matches the manifest-bound SHA-256 digest
- **THEN** the service MUST atomically persist the data and validated manifest
  state before replacing in-memory pricing

#### Scenario: Manifest, digest, redirect, or version validation fails

- **WHEN** the manifest is invalid, the pricing digest differs, pricing JSON is
  invalid, a redirect leaves the trusted allowlist, or the version is older
- **THEN** the service MUST NOT persist remote pricing data, replace in-memory
  pricing, or replace the validated local version
- **THEN** startup with no usable cache MUST load the bundled fallback resource

### Requirement: Default browser image policy must not permit arbitrary HTTP

The default Content-Security-Policy MUST allow local, data, blob, and HTTPS
images but MUST NOT permit arbitrary `http:` images. Any domain-specific
exception SHALL be an explicit deployment configuration override, not a
database-backed admin setting.

#### Scenario: A default-policy response is rendered

- **WHEN** an operator uses the default CSP policy
- **THEN** the `img-src` directive MUST allow `'self'`, `data:`, `blob:`, and
  `https:` sources but MUST NOT allow the `http:` scheme
- **THEN** any compatibility exception MUST require a reviewed deployment
  configuration change rather than an administrator system setting

### Requirement: Release automation must validate the active GoReleaser schema without publication credentials

The repository SHALL use the current GoReleaser archive and Docker v2 schema,
preserving release image/tag behavior while supporting validation without a tag,
registry login, image push, or GitHub Release publication. CI MUST run the
configuration validation with non-publishing placeholder values.

#### Scenario: CI validates release configuration

- **WHEN** the release configuration validation job runs on a normal pull
  request or branch build
- **THEN** `goreleaser check` MUST succeed with only placeholder release values
- **THEN** the job MUST NOT create tags, releases, registry credentials, or
  image pushes

## MODIFIED Requirements

### Requirement: First token and first output must have independent, stable semantics

The system SHALL define `first_token_ms` as the latency to the first non-empty
text, reasoning, or tool token-like downstream increment. `first_output_ms` and
`first_output_kind` SHALL represent the first downstream-consumable output of
any modality. Upstream stream circuit event aggregation and fallback introduced
by the v0.1.169 sync MUST NOT classify lifecycle, role-only, empty delta,
usage-only, finish-only, terminal-without-output, or error events as first token
or first output.

#### Scenario: Aggregated upstream disconnect precedes visible output

- **WHEN** an upstream stream disconnect is recorded or aggregated by the
  circuit breaker before a downstream-consumable output is emitted
- **THEN** the event MUST NOT create `first_token_ms` or `first_output_ms`
- **THEN** a later non-empty downstream text, reasoning, tool, or media output
  MUST continue to determine the applicable first-output timing
