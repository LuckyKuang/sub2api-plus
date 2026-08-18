## ADDED Requirements

### Requirement: Administrator self mode must retain existing personal operations

The system SHALL treat the authenticated administrator's own account as self
mode when no target is selected or when the selected target ID equals the
authenticated administrator ID. Self mode MUST continue to use the existing
personal routes and MUST retain their current create, read, update, delete,
credential-copy, purchase, redemption, profile, and asynchronous-image
capabilities. The role of the selected account MUST NOT determine self mode.

#### Scenario: Administrator uses personal API keys

- **WHEN** an administrator selects their own account and opens API keys
- **THEN** the existing owner API-key page and endpoints MUST be used
- **THEN** the administrator MUST retain all current operations on their own keys

#### Scenario: Administrator returns from support mode

- **WHEN** an administrator viewing another account selects their own account
- **THEN** the UI MUST return to the corresponding personal route
- **THEN** no support-mode restriction MUST remain on personal or administrator-management pages

#### Scenario: Administrator selects another administrator

- **WHEN** the selected target has role `admin` but a different user ID
- **THEN** the UI MUST use non-self support mode
- **THEN** the target MUST remain read-only

### Requirement: Non-self support views must be read-only by construction

The system SHALL expose non-self account support data only through an
administrator-authenticated support namespace. The backend MUST register no
support mutation routes, and support query code MUST NOT modify the target user
or any target-owned resource. The authenticated administrator MUST remain the
actor and the selected account MUST remain a separate target.

#### Scenario: Administrator reads another account

- **WHEN** an administrator requests a GET support resource for a non-deleted target
- **THEN** the system MUST return only the authorized read model
- **THEN** the audit record MUST identify the administrator as actor and the selected account as target

#### Scenario: Caller attempts a support mutation

- **WHEN** any caller sends POST, PUT, PATCH, or DELETE to the support namespace
- **THEN** no target data MUST change
- **THEN** the request MUST be rejected because no matching mutation route exists

#### Scenario: Target is disabled or deleted

- **WHEN** the target is disabled but not deleted
- **THEN** an administrator MUST still be able to inspect its support views
- **WHEN** the target is soft-deleted or missing
- **THEN** the support request MUST return not found

#### Scenario: Support response caching

- **WHEN** any support-namespace response is returned after target validation
- **THEN** it MUST include `Cache-Control: no-store`
- **THEN** target data MUST NOT be retained by shared or browser HTTP caches

#### Scenario: Non-administrator requests support data

- **WHEN** an authenticated non-administrator requests the support namespace
- **THEN** the request MUST be forbidden

### Requirement: Administrator target-key responses must not disclose credentials

The system SHALL return API-key metadata without plaintext credentials from
every administrator endpoint that enumerates a target user's keys. The ordinary
self endpoint SHALL remain unchanged for the authenticated owner. Frontend
support models and pages MUST NOT contain credential, copy, export, reveal, use,
or client-import capabilities.

#### Scenario: Administrator lists another user's keys

- **WHEN** an administrator lists keys through either the support endpoint or the existing administrator user-key endpoint
- **THEN** the response MUST include safe operational metadata
- **THEN** the response MUST NOT include `key`, secret, token, authorization, export, or full IP-list data

#### Scenario: Administrator lists their own keys as owner

- **WHEN** an administrator uses the ordinary `/keys` owner endpoint
- **THEN** the existing owner response and key-management operations MUST remain available

#### Scenario: Support key page renders

- **WHEN** the support API-key page displays another user's keys
- **THEN** it MUST omit plaintext values and all copy, export, create, edit, delete, status-change, group-change, and reset controls

### Requirement: Administrator asynchronous-image support must not use user credentials

The system SHALL let an administrator read a target user's durable
asynchronous-image task list and task detail by target user ID without loading
or using any target API-key value. These reads MUST NOT create, retry, cancel,
delete, download, repair, or otherwise change a task. Ordinary gateway task
ownership MUST remain scoped to the exact authenticated user and API key.

#### Scenario: Administrator lists target image tasks

- **WHEN** an administrator requests asynchronous-image history for another user
- **THEN** the repository MUST filter durable history by the target user ID
- **THEN** the response MAY include an API-key ID or name but MUST NOT include its value

#### Scenario: Administrator reads task detail

- **WHEN** an administrator requests a task that belongs to the selected user
- **THEN** the response MUST contain only the safe task read model
- **THEN** neither Redis execution state nor durable history MUST be modified

#### Scenario: Support image page renders

- **WHEN** the support asynchronous-image page displays another user's tasks
- **THEN** it MUST offer list filtering and read-only detail or preview
- **THEN** it MUST NOT offer submission, edit, retry, cancel, deletion, archive download, export, or reuse actions

#### Scenario: Administrator uses their own asynchronous-image page

- **WHEN** the selected account is the authenticated administrator
- **THEN** the existing API-key-authenticated asynchronous-image page MUST be used
- **THEN** all current owner operations MUST remain available

### Requirement: Support navigation must keep actor and target state unambiguous

The administrator sidebar SHALL default to the authenticated administrator and
SHALL offer searchable non-deleted accounts. Non-self pages MUST display a
persistent read-only target banner. The route target ID SHALL be authoritative,
and support selection MUST NOT overwrite the authentication store, token,
cookie, or session.

#### Scenario: Administrator selects another account

- **WHEN** the administrator selects a different non-deleted account
- **THEN** navigation MUST move to the matching support route for that target
- **THEN** a persistent banner MUST identify the target and read-only mode

#### Scenario: Administrator leaves support routes

- **WHEN** the administrator opens a normal administrator-management route
- **THEN** the support target MUST NOT change that route's authorization or mutation behavior

#### Scenario: Selected target disappears

- **WHEN** a support target is deleted and a subsequent request returns not found
- **THEN** the frontend MUST clear the target and return to self mode

#### Scenario: Administrator switches targets rapidly

- **WHEN** an earlier target request completes after a later target selection
- **THEN** the earlier response MUST be discarded
- **THEN** it MUST NOT replace the selected target or any current resource data

#### Scenario: Route contains an invalid target identifier

- **WHEN** a support route target is not a positive safe integer
- **THEN** the frontend MUST return to the corresponding self route
- **THEN** it MUST NOT render an empty or stale support page

### Requirement: Support usage must report real target statistics

The support usage view SHALL query usage statistics using the explicit target
user ID and SHALL NOT use placeholder administrator-user statistics. Period
filters MUST use the browser timezone and calendar boundaries matching their
labels.

#### Scenario: Administrator reads target usage

- **WHEN** an administrator opens another user's support usage page
- **THEN** totals MUST be computed from usage records filtered by that target ID
- **THEN** the response MUST contain only summary totals and timing metrics

#### Scenario: Administrator selects week or month

- **WHEN** the selected period is week
- **THEN** the start time MUST be Monday at midnight in the supplied user timezone
- **WHEN** the selected period is month
- **THEN** the start time MUST be the first day of that month at midnight in the supplied user timezone
