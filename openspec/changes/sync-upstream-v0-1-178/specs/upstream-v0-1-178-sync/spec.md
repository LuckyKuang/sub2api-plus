## ADDED Requirements

### Requirement: Official v0.1.178 and Plus release behavior must coexist

The repository SHALL integrate official v0.1.178 commit
`e0c48a19ed794a565e3858662520afe0a1f9f0ba` while retaining intentional Plus
identity, authorization, quota, observability, deployment, and distribution
behavior. Prepared metadata SHALL identify `v0.1.178+custom.001` and remain
unpublished.

#### Scenario: Release metadata is checked

- **WHEN** the embedded version, Docker defaults, and `UPSTREAM.md` are read
- **THEN** they SHALL identify official v0.1.178 and Plus `custom.001`
- **THEN** the mapping SHALL have status `planned`
- **THEN** no tag, GitHub Release, or image SHALL be published by this change

### Requirement: Imported Go source uses the Plus module path

All active Go source imported by the v0.1.178 merge SHALL use
`github.com/LuckyKuang/sub2api-plus`.

#### Scenario: The merged tree is searched

- **WHEN** active Go files are scanned for the official module path
- **THEN** no `github.com/Wei-Shaw/sub2api` import SHALL remain

### Requirement: New migrations are forward-only and uniquely named

Imported database changes SHALL be applied by immutable forward-only SQL files
with unique increasing prefixes. The Codex seed backfill SHALL retain its SQL
behavior under prefix 227 because the official tag contains two prefix-225
files.

#### Scenario: An upgraded database starts

- **WHEN** migrations 224, 225 pricing, 226 monitor quota, and 227 seed
  backfill are applied in lexical order
- **THEN** each migration SHALL run once, be idempotent where documented, and
  SHALL not alter an existing migration checksum
