## ADDED Requirements

### Requirement: Grok cross-client mapping must require explicit opt-in

The system SHALL disable Grok cross-client model mapping unless the persisted
setting is exactly `true`. Missing, empty, malformed, and explicit `false`
values MUST all resolve to disabled.

#### Scenario: An upgrade has no stored mapping setting

- **WHEN** an existing installation first loads settings after the upgrade and
  the mapping key is absent
- **THEN** cross-client mapping MUST be disabled
- **THEN** GPT and Claude model names MUST NOT be silently rewritten to Grok

#### Scenario: An administrator explicitly enables mapping

- **WHEN** the persisted mapping setting is exactly `true`
- **THEN** the system MAY apply the documented Grok cross-client mappings

### Requirement: Grok password OAuth must remain hard-disabled

The Grok password-to-SSO OAuth flow SHALL remain disabled at the service
boundary. A retained configuration field MUST NOT enable the flow, capabilities
MUST report disabled, and rejected requests MUST NOT contact the upstream login
client or expose the supplied password.

#### Scenario: A legacy configuration sets password authentication to true

- **WHEN** `gateway.grok.password_auth_enabled` is set to `true`
- **THEN** capabilities MUST still report `password_auth_enabled=false`
- **THEN** the password endpoint MUST reject the request before upstream login
- **THEN** logs and responses MUST NOT contain the submitted password
