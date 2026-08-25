## ADDED Requirements

### Requirement: New migrations are forward-only and uniquely named

Imported database changes SHALL be applied by immutable forward-only SQL files
with unique increasing prefixes after published Plus prefix `228`. Published
Plus files `224_backfill_codex_fingerprint_mode.sql`,
`225_channel_model_time_pricing.sql`, `226_channel_monitor_quota_mode.sql`, and
`228_user_platform_quotas_add_cn_providers.sql` SHALL keep their filenames and
checksums. Prefix `227` SHALL NOT be reused.

#### Scenario: An upgraded database starts

- **WHEN** migrations `229` usage-log indexes, `230` composite CN routes,
  `231` channel multipliers, `232` plugins, and `233` plugin artifacts are
  applied in lexical order after existing Plus 228
- **THEN** each migration SHALL run once, be idempotent where documented, and
  SHALL not alter an existing migration checksum
- **THEN** the usage-log index file SHALL keep the `_notx` suffix

### Requirement: Codex fingerprint seed persistence stays excluded

Official `225_backfill_codex_fingerprint_seed.sql`, seed lifecycle code,
repository SQL, and seed tests SHALL remain absent. No compensation migration
SHALL be added.

#### Scenario: The merged tree is searched for seed persistence

- **WHEN** migrations, repositories, and tests are scanned for
  `codex_fingerprint_seed`
- **THEN** no active read/write path, migration, or test SHALL reintroduce it

### Requirement: Plugin tables land with the complete plugin unit

Plugin SQL `232` and artifact SQL `233` SHALL ship together with plugin API,
proto/generated output, admin routes/UI, environment bindings, deploy examples,
and security/development docs.

#### Scenario: Plugin support is enabled in configuration

- **WHEN** an administrator inspects default configuration and deploy examples
- **THEN** the experimental OAuth transport plugin SHALL be documented and
  defaulted off
- **THEN** enabling it SHALL require explicit configuration rather than an
  implicit default
