# Revise IP login-failure controls

## Problem

The login-failure security controls contain one enforcement defect and three
management-plane behaviors that no longer match the required operation:

- The threshold transaction uses the source-IP parameter as both an inferred
  string and an explicitly typed `inet`. PostgreSQL rejects the threshold
  statement, rolls back the increment, and leaves the counter at one.
- The failure window is limited to 1,440 minutes while the related block
  duration supports up to one year.
- The failure-state table polls every 15 seconds even though the page already
  provides explicit refresh and refreshes after management actions.
- A quick manual block from the failure-state table inherits the automatic
  block duration and can return a temporary automatic rule when both actions
  race. Administrators require that action to create a permanent manual rule.

## Proposal

- Give every source-IP use in the threshold rule statement a consistent
  explicit text type before converting it to `inet` for containment checks.
- Allow both the login-failure window and automatic block duration to be
  configured from 1 through 525,600 minutes.
- Remove timer and visibility-triggered polling from the failure-state table.
  Preserve initial loading, explicit refresh, navigation, filtering, and
  post-mutation refreshes.
- Make the failure-state quick-block action create or upgrade an exact-IP,
  permanent `manual_block` with `expires_at = NULL`.
- Preserve the failure counter and keep automatic threshold blocks governed by
  `login_failure_block_minutes`.
- Serialize quick manual blocks with exact-IP threshold blocks and guarantee a
  permanent manual result even when an automatic block wins the race first.

## Non-goals

- Changing the generic rule editor, which may continue creating temporary or
  permanent manual rules.
- Changing the fixed-window counting algorithm or CAPTCHA ordering.
- Changing automatic block duration semantics.
- Adding a database migration.

## Impact

- Security settings validation and persisted-setting parsing change.
- The failure-state quick-block repository contract and implementations change.
- The IP access-control admin page, English and Chinese messages, and focused
  frontend tests change.
- Backend service, repository, handler, middleware, and integration tests
  change where they implement or verify the repository contract.
