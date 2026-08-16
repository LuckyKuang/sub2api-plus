## ADDED Requirements

### Requirement: Official v0.1.177 and Plus release behavior must coexist

The repository SHALL integrate official v0.1.177 commit
`073e92d17178a1ccdb0a27017f572f10c9c7ab62` while retaining intentional
Plus identity, authorization, quota, observability, deployment, and
distribution behavior. Prepared release metadata SHALL identify
`v0.1.177+custom.001` and SHALL remain unpublished.

#### Scenario: Prepared release metadata is checked

- **WHEN** embedded version and `UPSTREAM.md` are inspected
- **THEN** both SHALL identify official `v0.1.177` and Plus
  `v0.1.177+custom.001`
- **THEN** no tag, Release, or image SHALL be represented as published

### Requirement: Official Go imports must follow the Plus module path

All active Go source SHALL import the Plus module path after the merge.

#### Scenario: New official files are imported

- **WHEN** v0.1.177 repository, service, and test files are compiled
- **THEN** their internal imports SHALL use
  `github.com/LuckyKuang/sub2api-plus`
- **THEN** active Go source SHALL contain no
  `github.com/Wei-Shaw/sub2api` imports

### Requirement: Daily rollups must use server-configured timezone

Group daily-usage rollups SHALL use the application timezone resolved in the
order `TZ`, `TIMEZONE`, then configuration/default. Browser timezone input
SHALL NOT define persistent bucket boundaries.

#### Scenario: The configured timezone changes

- **WHEN** stored rollup timezone differs from the current application timezone
- **THEN** prior derived buckets SHALL be rebuilt using the new timezone
- **THEN** today and yesterday costs SHALL use the same bucket definition

#### Scenario: Source usage changes after aggregation

- **WHEN** recompute, cleanup, or partition deletion affects a rolled-up day
- **THEN** that day SHALL be invalidated
- **THEN** startup or scheduled synchronization SHALL rebuild the missing data

### Requirement: Plus-owned workflows and identity must survive the merge

CI and release workflows SHALL keep Plus repository identity, pinned actions,
dynamic toolchain sources, and custom release naming. Codex outbound identity
SHALL retain the credential-owner account/global/compiled-default precedence.

#### Scenario: A shadow account is selected

- **WHEN** the shadow account uses credentials owned by another account
- **THEN** outbound identity selection SHALL start with the credential owner's
  valid `credentials.user_agent`
- **THEN** fingerprint, beta-feature, and generic-header handling SHALL NOT
  change the selected client family or identity source
