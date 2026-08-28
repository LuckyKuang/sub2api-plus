## ADDED Requirements

### Requirement: Unnecessary system-generated third-party identifiers must be neutral

Third-party URL paths, query parameters, headers, and default request bodies constructed by the gateway SHALL NOT add an unnecessary case-insensitive `sub2api` protocol identifier. This applies to non-provider usage/check UAs, updater UAs, OAuth referral parameters, synthetic connectivity text, injected technical prompt markers, generated validation tokens, batch display names, and object-storage healthcheck keys.

Official provider CLI/OAuth identity profiles and the Codex identity precedence contract SHALL remain unchanged where required for provider compatibility. Product/payment subjects and other user-visible product identity are outside this technical protocol cleanup.

#### Scenario: Gateway constructs an API-key upstream request

- **WHEN** the gateway builds system headers or query parameters for a third-party provider
- **THEN** it MUST NOT add a project-branded UA, `referrer=sub2api`, or `X-Sub2API-*` system header
- **THEN** a provider-required official identity profile MUST remain coherent and unchanged

#### Scenario: Gateway generates technical test content

- **WHEN** account validation synthesizes connectivity text, a token, a batch name, or a storage key
- **THEN** the generated technical value MUST be brand-neutral
- **THEN** product/payment copy MUST remain branded as before

### Requirement: User and model content must not be rewritten

The implementation SHALL NOT globally search or replace request bodies, response bodies, SSE events, prompts, tool arguments, credentials, arbitrary user fields, or model output to remove the product name.

#### Scenario: User content contains the product name

- **WHEN** a user prompt or tool argument contains `Sub2API Plus`
- **THEN** the value MUST be forwarded with its original bytes and casing
- **THEN** a returned model value MUST not be filtered because of that text

### Requirement: Reserved outbound header names must be blocked narrowly

Account header-override persistence and the final outbound request boundary SHALL reject or remove reserved project-specific header names such as `X-Sub2API-*`. The implementation MUST NOT reject an otherwise valid custom header solely because its value contains the user-visible product name.

The gateway SHALL consume `X-Grok-Client-Tool-Cache` only as an inbound,
request-scoped Grok routing control. It is not an official provider header and
MUST be rejected from account/channel-monitor header configuration and removed
from HTTP, plugin, and WebSocket upstream requests.

#### Scenario: Administrator saves a reserved header name

- **WHEN** an administrator submits an override named `X-Sub2API-Trace`
- **THEN** persistence MUST reject the override
- **THEN** the header MUST not be sent upstream

#### Scenario: Administrator saves legitimate branded content

- **WHEN** an administrator submits `X-Organization: Sub2API Plus`
- **THEN** the override MUST pass brand-specific validation
- **THEN** ordinary header safety and provider rules MAY still apply

#### Scenario: Client selects Grok client-tool cache behavior

- **WHEN** a custom client sends `X-Grok-Client-Tool-Cache`
- **THEN** the gateway MAY use its recognized value as a local routing control
- **THEN** no upstream HTTP, plugin, or WebSocket request may contain that header
