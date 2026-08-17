## ADDED Requirements

### Requirement: Owners may delete only failed asynchronous image tasks

The system SHALL allow an authenticated API key to delete a task only when the
task belongs to the same user and API key and its status is `failed`. Successful
deletion MUST return HTTP 204. A missing task or a task outside that owner scope
MUST return HTTP 404 without disclosing its existence. A task in any other
status MUST return HTTP 409 and remain unchanged.

#### Scenario: Owner deletes a failed task

- **WHEN** an API key deletes its own task whose status is `failed`
- **THEN** the endpoint MUST return HTTP 204
- **THEN** the task MUST no longer appear in that API key's history

#### Scenario: Owner attempts to delete an active task

- **WHEN** an API key deletes its own task whose status is `processing`
- **THEN** the endpoint MUST return HTTP 409
- **THEN** both Redis state and PostgreSQL history MUST remain unchanged

#### Scenario: API key requests another key's task

- **WHEN** an API key deletes a task owned by another API key, including one
  belonging to the same user
- **THEN** the endpoint MUST return HTTP 404
- **THEN** the task MUST remain unchanged

### Requirement: Failed task deletion must remain recoverable

The system SHALL delete the exact Redis execution key before conditionally
deleting the owner-scoped failed PostgreSQL history row. A missing Redis key
MUST count as success. A Redis error MUST leave PostgreSQL history intact so the
operation can be retried. Deletion MUST NOT scan or remove S3-compatible object
storage.

#### Scenario: Redis task state has already expired

- **WHEN** an owned failed history row exists but its Redis key does not
- **THEN** deletion MUST still remove the history row and return HTTP 204

#### Scenario: Redis deletion fails

- **WHEN** deleting the Redis key returns an infrastructure error
- **THEN** the endpoint MUST return HTTP 503
- **THEN** the PostgreSQL history row MUST remain available for retry

#### Scenario: Failed task has object references

- **WHEN** an owned failed task is deleted
- **THEN** its task records MUST be removed from Redis and PostgreSQL
- **THEN** no object-storage object MUST be scanned or deleted

### Requirement: Task management must not require billable request capacity

The system SHALL apply the authenticated task-management path classification to
list, detail, download, and deletion routes under both `/v1/images/tasks` and
`/images/tasks`. These routes MUST bypass billing enforcement while retaining
API-key credential, hard-disabled-key, user-state, IP restriction, and owner
scoping checks.

#### Scenario: Valid key with no billable balance deletes its failed task

- **WHEN** an otherwise valid API key without billable request capacity sends
  an owner-authorized task deletion
- **THEN** billing enforcement MUST NOT reject the request
- **THEN** normal status and ownership policy MUST still apply

#### Scenario: Missing credentials request task management

- **WHEN** a caller without valid API-key credentials requests a task list or
  deletion
- **THEN** authentication MUST reject the request

### Requirement: Task filters must use consistent controls

The asynchronous image task list SHALL render its API Key and status filters
with the shared Select control while preserving their current values, change
behavior, responsive grid widths, and accessible labels. Their triggers MUST
have matching height, vertical text alignment, and chevron alignment without
shifting between enabled, disabled, or loading states.

#### Scenario: Filters render together on desktop

- **WHEN** the task page renders at a desktop width
- **THEN** both filter triggers MUST align within one pixel at their top and
  bottom edges
- **THEN** long API-key labels MUST truncate without changing trigger size

#### Scenario: Filters render on a narrow viewport

- **WHEN** the task page renders at a 375 pixel viewport width
- **THEN** both filters MUST remain fully contained and usable without overlap

### Requirement: Failed-row deletion must preserve list context

The task list SHALL show a delete action only for failed rows and SHALL require
confirmation before calling the API with the currently selected API key. Only
the selected row SHALL show deletion progress. On failure, the row, filters,
and page MUST remain unchanged. On success, the current page MUST refresh and an
empty non-first page MUST fall back to the preceding page.

#### Scenario: User cancels failed-row deletion

- **WHEN** a user opens deletion confirmation and cancels
- **THEN** no deletion request MUST be sent
- **THEN** the list state MUST remain unchanged

#### Scenario: Deletion empties a later page

- **WHEN** the only row on a non-first page is deleted successfully
- **THEN** the list MUST load the immediately preceding page

#### Scenario: Deleted task detail is open

- **WHEN** deletion succeeds for the task currently shown in the detail dialog
- **THEN** that detail dialog MUST close and discard the deleted task
