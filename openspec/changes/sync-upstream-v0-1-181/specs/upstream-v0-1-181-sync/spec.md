## ADDED Requirements

### Requirement: Official v0.1.181 and Plus release behavior must coexist

The repository SHALL integrate official v0.1.181 commit
`3af5443b224823ae507a50c7b113aa50604409c8` while retaining intentional Plus
identity, authorization, quota, observability, deployment, and distribution
behavior. Prepared metadata SHALL identify `v0.1.181+custom.001` and remain
unpublished.

#### Scenario: Release metadata is checked

- **WHEN** the embedded version, Docker defaults, and `UPSTREAM.md` are read
- **THEN** they SHALL identify official v0.1.181 and Plus `custom.001`
- **THEN** the mapping SHALL have status `planned`
- **THEN** no tag, GitHub Release, or image SHALL be published by this change

### Requirement: Imported Go source uses the Plus module path

All active Go source imported by the v0.1.181 merge SHALL use
`github.com/LuckyKuang/sub2api-plus`. The Go toolchain SHALL be `1.27.0`.

#### Scenario: The merged tree is searched

- **WHEN** active Go files are scanned for the official module path
- **THEN** no `github.com/Wei-Shaw/sub2api` import SHALL remain
- **THEN** `backend/go.mod` SHALL declare Go `1.27.0`

### Requirement: Official 179–181 product capabilities are present

The merged tree SHALL include adaptive Chinese-provider protocols, channel
Fast/Flex/context multipliers, Composite Codex and CN routing,
`POST /v1/responses/input_tokens`, usage GROUPING SETS aggregation, Fast
`service_tier` on Responses/Chat/WS, OpenAI reset-card auto-use, configurable
model-list read limits, marketplace time pricing, experimental OAuth transport
plugins defaulted off, and the v0.1.181 Gemini/Grok/Responses Lite/status
cleanup fixes.

#### Scenario: Core official capabilities are inventoried

- **WHEN** routes, services, migrations, admin UI, and tests for those
  capabilities are inspected
- **THEN** each capability SHALL be present with matching tests
- **THEN** plugin configuration SHALL default to disabled
