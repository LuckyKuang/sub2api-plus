# Authentication and Passkeys

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
