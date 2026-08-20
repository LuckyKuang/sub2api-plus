## ADDED Requirements

### Requirement: Channel monitor quota mode is explicit and persisted

Channel monitors SHALL support `probe`, `quota`, and `quota_probe` modes. Quota
mode SHALL use the linked account snapshot and SHALL persist normalized quota
data in monitor history without requiring an LLM probe.

#### Scenario: An administrator selects quota mode

- **WHEN** a monitor is saved with `check_mode: quota` and a linked account
- **THEN** the monitor SHALL retain both values and a run SHALL record a quota
  snapshot or an actionable missing-account result

### Requirement: Time pricing is evaluated from persisted channel configuration

Channel model pricing MAY include recurring daily time periods and multipliers.
The schema and evaluation SHALL be forward-compatible with existing pricing
rows, which continue to use their original values when no period matches.

#### Scenario: Existing pricing has no time periods

- **WHEN** a request is billed against a legacy pricing row
- **THEN** the existing base pricing behavior SHALL remain unchanged

### Requirement: Frontend locales remain aligned

English and Chinese locale trees SHALL expose the same keys for monitor quota,
CN-provider balance/quota, and channel time-pricing controls.

#### Scenario: Locale parity is checked

- **WHEN** locale key parity tests run
- **THEN** neither locale SHALL contain a key missing from the other
