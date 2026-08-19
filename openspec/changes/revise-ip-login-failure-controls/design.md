# Design: revised IP login-failure controls

## Threshold transaction

The automatic-rule `INSERT ... SELECT` previously used one PostgreSQL bind
parameter both as an untyped value for the varchar `ip_or_cidr` column and as
an explicitly cast `inet` value in allow/block containment checks. PostgreSQL
could not infer a single type when the threshold branch first ran, so it
rejected the statement and rolled back the counter increment that reached the
threshold. Every occurrence now establishes the parameter as text, with
containment checks applying the subsequent text-to-inet cast. The fixed-window
counting and automatic block-duration algorithms are otherwise unchanged.

## Setting bounds

The window and automatic block duration use the same inclusive bound of 1 to
525,600 minutes. A shared backend constant is the source of truth for request
validation and persisted-setting parsing. The frontend number input exposes the
same maximum. Existing values within the old range remain unchanged; invalid
persisted values still fall back to the existing safe default.

A one-year window can retain failure-state rows longer than the previous
one-day maximum. The existing bounded cleanup task already derives its cutoff
from the configured window, so no schema or cleanup algorithm change is needed.

## Failure-state refresh

The table loads when the page mounts and when an administrator explicitly
refreshes, searches, paginates, changes page size, or completes a related
management action. It does not register a timer or visibility listener. The
existing request sequence remains in place so overlapping explicit requests
cannot let an older response overwrite a newer response.

## Permanent quick manual block

The dedicated failure-state action does not accept or calculate a duration. It
continues validating administrator identity, runtime enforcement, the trusted
client-IP chain, deployment emergency allows, and persisted allow rules.

Within one transaction, the repository acquires the exact-IP advisory lock,
records whether any effective block already covered the IP, retires an expired
exact active manual row, and inserts or updates the exact active
`manual_block`. The upsert always writes `expires_at = NULL`. An existing exact
temporary manual block is therefore upgraded in place. A covering CIDR block or
an exact automatic block does not suppress creation of the permanent exact
manual rule. Once that manual rule exists, an exact active automatic rule is
marked released in the same transaction, with the administrator recorded as
the releasing actor. This preserves rule history without leaving a hidden
automatic rule that would keep blocking the IP after the permanent manual rule
is later released. Covering CIDR rules remain independent and are not changed.

`already_blocked` describes the state before this action. It may be true while
the returned rule is the newly created or upgraded permanent manual rule. This
keeps retries idempotent without weakening the permanent-result guarantee.

An ambiguous transaction commit is confirmed by querying the exact active
permanent manual rule and verifying that no exact automatic rule remains
active, rather than accepting any effective temporary block. Failure state is
never deleted by this action. Automatic blocks continue to calculate their own
expiry from `login_failure_block_minutes`.

## Management UI

The confirmation states that the block is permanent and that the failure
counter is preserved. Opening the dialog no longer reloads settings solely to
obtain a duration, and a settings-load failure no longer disables an otherwise
valid quick block. The server remains authoritative for all security checks at
submission time.
