# Authentication and Passkeys

## First Installation

The web and CLI setup wizards require a login-valid email and a password of
8–72 UTF-8 bytes (bcrypt's input limit). Multi-byte characters count by bytes,
not character count; the web wizard checks the same limits before submission.
The login form accepts nonempty passwords and delegates authentication to the
server, so a valid multi-byte setup password also works at sign-in.

With `AUTO_SETUP=true`, unset `ADMIN_EMAIL` and `ADMIN_PASSWORD` are generated
independently on an empty database. The random email is a login identifier;
retrieve generated credentials from the first-start logs and keep them private.
Explicit credentials are validated before inserting an administrator. Invalid
email addresses and passwords outside the byte limits stop fresh installation.

If any users or an administrator already exist, initialization skips account
creation and credential validation. Upgrades never reset existing credentials,
create a replacement administrator, or fail because of old bootstrap values.
`default.admin_email` / `default.admin_password` are legacy configuration fields;
use the setup wizard or `ADMIN_EMAIL` / `ADMIN_PASSWORD` for initialization.

## Passkeys

Passkey sign-in is optional. It requires both a valid WebAuthn relying-party
configuration and an administrator enabling Passkeys in System Settings. Until
both conditions are met, Passkey endpoints return the disabled-feature error
and existing password, TOTP, and OAuth sign-in paths are unchanged.

## Routes

The public sign-in ceremony uses:

- `POST /api/v1/auth/passkey/login/begin`
- `POST /api/v1/auth/passkey/login/finish`

Authenticated users manage their own credentials through the `/api/v1/user/passkeys`
routes. Registration and deletion remain password-gated, and WebAuthn user
verification is required for sign-in.

## Security Behavior

Global IP access control runs before every Passkey endpoint. A blocked source
cannot start or complete a ceremony. After a successful Passkey assertion, the
server clears that source IP's local login-failure streak, records the login,
and only then issues the normal token pair. If the IP failure-state store is
unavailable, this cleanup fails closed with `503 IP_ACCESS_CONTROL_UNAVAILABLE`;
no token is issued.

Failed Passkey assertions remain protected by the endpoint rate limit. They do
not enter the password/TOTP automatic IP-block counter without a separate
policy change.

Passkey assertion and registration completion request bodies are intentionally
omitted from audit-log request-body storage because they contain WebAuthn
credential material.

## Persistence and Rollback

Passkey storage is introduced by the forward-only
`196_passkey_credentials.sql` migration. The tables are additive, so rolling
back the application binary is schema-safe; users can use their existing login
methods while a prior binary is running.

## Email Verification Codes

Ordinary email verification and the extra notification-email verification share one
generation-bound cache primitive. A submission snapshots the current code and its
generation, atomically reserves an attempt against that generation (carrying over
legacy `Attempts` counters and enforcing the five-comparison cap), compares the code
in constant time, and only then atomically consumes exactly that generation. Missing,
expired, exhausted, replaced or unavailable code entries never return success, and a
reservation never extends the code's validity. Resending a code installs a new
generation, so a request holding the previous snapshot can neither spend the new
code's budget nor delete it. External error codes (`INVALID_VERIFY_CODE`,
`VERIFY_CODE_TOO_FREQUENT`, `VERIFY_CODE_MAX_ATTEMPTS`) and the resend cooldown are
unchanged.

## Password Reset Tokens

Password reset cache entries store only the hex-encoded SHA-256 hash of the token and
are consumed through an atomic compare-and-delete, so a reset link is single-use even
under concurrent submissions. Cache contents and exception logs never contain the raw
token.

Links issued before this change stored the token in plaintext. They no longer
validate and are rejected as invalid or expired; this is intentional fail-closed
behavior and there is no plaintext compatibility path. Affected users request a new
link from the sign-in page. Token TTL (30 minutes) and the reset-email cooldown
(30 seconds) are unchanged.

## Provider Account OAuth Reauthorization

Provider account detail and compact-list responses expose
`oauth_credential_owner`, an opaque SHA-256 fingerprint of the saved OAuth
credential owner, proxy route and recovery state. It contains no credential
material. Clients must capture it when starting reauthorization and send it as
`expected_owner` to
`POST /api/v1/admin/accounts/:id/apply-oauth-credentials` alongside the new
`type`, `credentials` and optional `extra`. A missing or malformed fingerprint
returns `400 OAUTH_OWNER_REQUIRED`; a changed owner returns
`409 OAUTH_OWNER_CHANGED`. Restart authorization after a conflict; adopting the
latest fingerprint and retrying an old result would authorize the wrong flow.

The final write compares the complete captured owner under database locks.
Credentials, supported Extra updates, recovery state and the scheduler outbox
commit atomically. A failed comparison or storage failure publishes no partial
credential update or recovery. Token-cache invalidation and local runtime
recovery occur only after successful commit. Recovery clears the captured
failure state while preserving manual scheduling pauses, expiration settings
and newer failures. Single and batch OpenAI/Grok refresh use the same owner
comparison, but retain their existing behavior of not clearing error/cooldown
state merely because token refresh succeeded.

Grok recovery and OpenAI API Key reset also advance the repository-owned
`extra.runtime_recovery_generation` in that same transaction. Scheduler
snapshots carry it through the existing outbox, so a process retaining an older
local model cooldown admits the recovered account. Imports and ordinary account
or Extra edits cannot supply, replace or erase this value. Grok observations
bind their local effects to the generation returned by their atomic database
commit: a failure committed before reset cannot restore the old cooldown, while
a valid new failure committed afterward still blocks normally. This field does
not select provider identity or alter Codex credential precedence.

The separate clear-rate-limit and clear-temp-unschedulable actions retain their
own clearing scopes and preserve account status/error, manual pauses and
expiration configuration. Each commits its scheduling changes and outbox
together. Successful-test recovery clears the applicable saved error/runtime
state and does no write when none exists; it does not enable a disabled
account. Cache/token/runtime effects wait for commit. Temporary-status reads
validate the saved account state before using a matching cached value, so a
late cache write cannot show a recovered or deleted account as active.

Browser flows also discard late results after account/owner changes, proxy
changes, closing or unmounting the authorization panel. This guards UI state;
the server-side owner comparison is the persistence boundary. A previously
submitted request may commit if it still owns the saved account.

Native refresh and reauthorization availability follows the implemented
provider contract: DeepSeek and Zhipu have no business refresh action, and
Zhipu has no existing-account UI relink action. See the
[supported business inventory](SEVEN_PROVIDER_SUPPORTED_BUSINESS.md) and
[native authentication contracts](providers/DOMESTIC_OAUTH.md).
