## ADDED Requirements

### Requirement: Public billing-interop endpoint must not exist

The gateway SHALL NOT register `GET /v1/sub2api/billing` or any replacement, alias, redirect, or migration-hint endpoint for private key-billing metadata. Authenticated and unauthenticated callers SHALL receive an ordinary HTTP 404. The response MUST NOT advertise `sub2api.key_billing` or a successor path.

#### Scenario: Former billing path is requested

- **WHEN** a client requests `GET /v1/sub2api/billing` with or without an API key
- **THEN** the server MUST respond with HTTP 404
- **THEN** the response MUST NOT expose billing metadata or migration instructions

### Requirement: Upstream billing probe capability must be removed

The system SHALL NOT schedule or run an upstream billing probe, persist probe snapshots, auto-enable probing for API-key accounts, or synchronize `accounts.rate_multiplier` from a probe. Runtime production code MUST NOT interpret `sub2api.key_billing`, `upstream_billing_probe`, `upstream_billing_probe_enabled`, or `upstream_billing_rate_sync_enabled`.

#### Scenario: Administrator manages an API-key account

- **WHEN** an administrator lists, creates, edits, or bulk-edits API-key accounts
- **THEN** the UI and API MUST NOT expose upstream declared-rate, probe, or rate-sync controls
- **THEN** a supplied manual `rate_multiplier` MUST remain editable without a probe-sync conflict
- **THEN** creating or changing the account MUST NOT trigger a probe

#### Scenario: Scheduler ranks API-key accounts

- **WHEN** the scheduler orders eligible API-key accounts
- **THEN** it MUST NOT read an upstream probe snapshot or probe-derived rate
- **THEN** unrelated OAuth scheduling-rate behavior MUST remain unchanged

### Requirement: Probe admin APIs and settings must not exist

Routes under `/admin/accounts/upstream-billing-probe` and `/admin/accounts/:id/upstream-billing-probe` SHALL be unregistered. Setting `upstream_billing_probe_settings` SHALL be deleted and MUST NOT be loaded or saved.

#### Scenario: Former probe management surface is used

- **WHEN** a client calls a former probe admin route or an administrator opens settings
- **THEN** the route MUST return HTTP 404 and the settings UI MUST NOT show a probe card
- **THEN** no successor probe payload or control MUST be offered

### Requirement: Manual account rate must survive probe removal

`accounts.rate_multiplier` SHALL remain an ordinary manually editable billing input. Probe cleanup MUST NOT assign, reset, or rescale it.

#### Scenario: Probe persistence is migrated away

- **WHEN** the forward migration runs on an account with retired probe keys and `rate_multiplier = 0.4`
- **THEN** the retired keys and global probe setting MUST be absent
- **THEN** `rate_multiplier` MUST still equal 0.4
- **THEN** scheduler cache invalidation MUST be enqueued for changed accounts

### Requirement: Import must discard retired probe extra keys

Account import SHALL strip the three retired probe keys before persistence. Ordinary account services and scheduler cache projections MUST NOT keep compatibility readers for them.

#### Scenario: Old backup is re-imported

- **WHEN** an administrator imports an account whose `extra` contains retired probe keys
- **THEN** the created account MUST persist without those keys
- **THEN** a supplied manual `rate_multiplier` MUST still be stored

### Requirement: Historical records are retained

Historical audit logs, usage logs, ops records, and existing on-disk backups SHALL NOT be rewritten or deleted by this change.

#### Scenario: A historical row names the former endpoint

- **WHEN** an administrator queries historical records
- **THEN** the existing row MUST remain available unchanged
