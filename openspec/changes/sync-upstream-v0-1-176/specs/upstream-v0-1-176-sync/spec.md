## ADDED Requirements

### Requirement: Official v0.1.176 and Plus release behavior must coexist

The repository SHALL integrate official v0.1.176 commit
`e803e3851c0a7e222cfadeafad7b8636ab959d11` while retaining intentional
Plus identity, session-policy, quota, migration, deployment, and
distribution behavior. Prepared release metadata MUST identify
`v0.1.176+custom.001` and MUST NOT imply that a tag, Release, or image
has already been published.

#### Scenario: Release preparation is validated

- **WHEN** embedded version, Docker arguments, deployment examples,
  release notes, and UPSTREAM.md are checked after implementation
- **THEN** they MUST agree on `v0.1.176+custom.001` and official
  `v0.1.176`
- **THEN** the upstream mapping status MUST remain non-published until
  the separate publication workflow succeeds

### Requirement: Imported migrations must preserve published Plus history

Official v0.1.176 migrations SHALL use unique, increasing Plus prefixes
after the published local maximum `219`. Published migration files MUST
remain immutable.

#### Scenario: Official 221 collides with Plus numbering policy

- **WHEN** official `221_group_model_pricing.sql` is imported
- **THEN** the resolved file MUST be `220_group_model_pricing.sql`
- **THEN** the SQL MUST add `long_context_pricing_enabled` defaulting to
  true and nullable `model_pricing`
- **THEN** no published Plus migration through `219` may be rewritten

### Requirement: Frontend dependency locking must preserve Plus ownership

The merged frontend lockfile SHALL remain the Plus graph. Official
v0.1.176 SHALL NOT replace it.

#### Scenario: The official release does not change frontend dependencies

- **WHEN** official 173→176 changes neither `frontend/package.json` nor
  `frontend/pnpm-lock.yaml`
- **THEN** the merge MUST retain the Plus lock graph
- **THEN** `pnpm install --frozen-lockfile` MUST succeed

### Requirement: Group pricing fields from both sides must remain addressable

A group SHALL expose Plus five-hour, live, and profit-control fields
together with official per-model pricing and the long-context ladder
switch.

#### Scenario: An administrator saves group model pricing

- **WHEN** a group stores `model_pricing` and
  `long_context_pricing_enabled`
- **THEN** lookup MUST use Group → Channel → built-in order
- **THEN** existing five-hour and profit-control configuration MUST
  remain intact
