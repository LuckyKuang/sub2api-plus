## ADDED Requirements

### Requirement: Enabled account uses a coherent Codex client profile

When an OpenAI OAuth account enables `codex_cli_only`, the system SHALL allow
the request only when it matches a built-in official Codex client profile or an
explicit administrator-configured compatibility profile. A built-in profile
MUST require both User-Agent and Originator, an exact coherent leading client
identity, and a valid Codex semantic version. It MUST NOT authorize a request
from only one header, a UA substring, a UA trailing identity, or a
case-insensitive rewrite of the `Codex ` family.

#### Scenario: canonical profile permits a supported product surface

- **WHEN** a request carries a reviewed canonical Originator and an exact
  matching User-Agent leading identity with a valid version and known evidence
- **THEN** it matches the corresponding built-in official profile
- **AND THEN** an enabled account may be used.

#### Scenario: mismatched identity is rejected

- **WHEN** a request has an otherwise known Codex User-Agent but a missing or
  different Originator
- **THEN** it does not match an official profile
- **AND THEN** an enabled account is denied.

### Requirement: Codex header evidence is closed and non-empty

An enabled-account profile decision SHALL accept only explicitly known,
non-empty Codex evidence header names. An arbitrary header whose name merely
starts with `x-codex-` SHALL NOT satisfy the evidence requirement. Empty or
operator-disabled generic evidence rules SHALL NOT make the enabled-account
gate fail open.

#### Scenario: fake prefix is insufficient

- **WHEN** a request uses `X-Codex-Fake` but no known non-empty evidence header
- **THEN** it is denied for an enabled account.

### Requirement: Historical Codex profile compatibility is explicit and closed

The system SHALL keep `codex_app`, `codex_exec`, `codex_sdk_ts`, and
`codex_vscode_copilot` outside the built-in official registry. It SHALL admit
one of these historical identities only when the global legacy-profile
compatibility setting is enabled. A historical identity MUST require the same
exact case-sensitive leading User-Agent/Originator equality, complete semantic
version, known non-empty Codex evidence, and configured version bounds as a
built-in profile. It MUST be reported as a legacy-compatible profile and MUST
NOT be reported as an official profile.

#### Scenario: compatibility mode is disabled by default

- **WHEN** an enabled account receives a coherent request with
  `originator=codex_exec` and known evidence while the global switch is false
- **THEN** the request is denied as an unmatched profile.

#### Scenario: compatibility mode admits only the closed migration set

- **WHEN** the global switch is true and an enabled account receives a
  coherent, evidence-bearing `codex_exec` request with a valid version
- **THEN** the request is admitted as `codex-legacy-compatible`
- **AND THEN** it is not classified as an official profile.

#### Scenario: compatibility mode remains exact

- **WHEN** the global switch is true and a request uses `CODEX_EXEC`,
  `codex_exec_evil`, a mismatched Originator, incomplete version, or only a UA
  trailer
- **THEN** it is denied.

### Requirement: Inbound authorization cannot be globally forced

`gateway.force_codex_cli` SHALL NOT alter the allow/deny outcome of an inbound
Codex client-profile decision. Generic App Server allow switches and
per-account generic App Server overrides SHALL NOT authorize an otherwise
unknown request.

#### Scenario: force mode does not bypass account policy

- **WHEN** force-Codex outbound behaviour is enabled and a request does not
  match any allowed profile
- **THEN** an enabled account is denied.

### Requirement: Every OpenAI-compatible ingress enforces the profile gate

The system SHALL enforce the enabled-account Codex client-profile decision for
Responses HTTP, Chat Completions, Anthropic Messages, Anthropic Messages Count
Tokens, Alpha Search, and Responses WebSocket before it obtains an upstream
credential or opens an upstream connection.

#### Scenario: Messages cannot bypass the profile gate

- **WHEN** a non-matching client calls `/v1/messages` against an enabled OpenAI
  OAuth account
- **THEN** the request is denied before conversion or upstream forwarding.

#### Scenario: Count Tokens and Alpha Search skip an ineligible candidate

- **WHEN** `/v1/messages/count_tokens` or `/alpha/search` first selects an
  enabled account whose client-profile policy does not match the request
- **THEN** the gateway releases that candidate's acquired slot without marking
  it unhealthy
- **AND THEN** it may select and forward through another eligible account
- **OR THEN** it denies the request locally when no eligible account remains,
  before credential retrieval or upstream forwarding.

#### Scenario: WebSocket candidate is ineligible

- **WHEN** the first `response.create` frame selects an enabled account and the
  request does not match its client policy
- **THEN** that account is not used and no account health failure is recorded
- **AND THEN** another eligible account may be selected
- **OR THEN** the client receives a WebSocket policy-violation closure when no
  eligible account remains.

### Requirement: OAuth session sharing remains isolated from API-key accounts

The OpenAI OAuth session-sharing policy SHALL apply only when the selected
account has `PlatformOpenAI` and OAuth account type with sharing enabled. An
OpenAI API-key account SHALL retain API-key-scoped upstream session identifiers
and group-local `previous_response_id` bindings, even when historic account
data contains an OAuth session-policy field. A response identifier in the
OAuth shared namespace SHALL NOT reject, redirect, or otherwise affect an API
key account selection or continuation.

#### Scenario: API-key continuation ignores historic OAuth policy data

- **WHEN** an OpenAI API-key account contains historic OAuth session-policy
  data and has a group-local response binding
- **THEN** its upstream session identifier remains scoped to the authenticated
  API key
- **AND THEN** its `previous_response_id` continuation uses the local binding
  without evaluating the OAuth allow-list.

#### Scenario: foreign shared response does not block API-key selection

- **WHEN** a request's `previous_response_id` exists in an OAuth shared
  namespace owned by another user or policy scope
- **AND WHEN** an OpenAI API-key account is selected for the request
- **THEN** the API-key selection is allowed without reading or applying that
  OAuth shared response policy.

#### Scenario: rejected shared OAuth candidate does not block an API-key fallback

- **WHEN** a shared OAuth candidate is rejected by its response ownership or
  allow-list boundary
- **AND WHEN** a compatible OpenAI API-key account remains available
- **THEN** the gateway releases and excludes only that OAuth candidate
- **AND THEN** it selects the API-key account without applying the OAuth
  sharing policy to it.

### Requirement: Admin UI describes feature restriction accurately

The account editor SHALL describe the option as an official Codex request
profile restriction and SHALL state that request features are spoofable and do
not attest a client binary or determine account sharing. English and Chinese
locale entries SHALL provide equivalent meaning.

#### Scenario: administrator reviews the restriction explanation

- **WHEN** an administrator opens the OpenAI OAuth account editor
- **THEN** the option states that it allows approved Codex request profiles
- **AND THEN** it explains that headers cannot attest a binary or independently
  determine account sharing in both English and Chinese.

### Requirement: Official profile updates are reviewed and versioned

Built-in profile additions or changes SHALL include a cited OpenAI upstream
source or release reference and regression fixtures for the observed wire
identity. The running service SHALL NOT dynamically download or automatically
activate remote profile rules.

#### Scenario: an upstream profile changes

- **WHEN** a maintainer adds or changes a built-in profile
- **THEN** the change includes a reviewed OpenAI upstream source or release
  reference and a matching regression fixture
- **AND THEN** the running service uses only the released source-controlled
  registry rather than downloading remote rules.
