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
with unique increasing prefixes. The latest Plus Codex mode migration SHALL
remain at prefix 224, and the imported CN-provider quota constraint SHALL use a
unique later prefix.

#### Scenario: An upgraded database starts

- **WHEN** migrations 224 mode backfill, 225 pricing, 226 monitor quota, and
  228 CN-provider quota constraint are applied in lexical order
- **THEN** each migration SHALL run once, be idempotent where documented, and
  SHALL not alter an existing migration checksum
