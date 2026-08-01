## Baseline and release identity

The official baseline is `v0.1.168` at
`99c8e4bf7564823bafbab369acab6539e734c1bb`. Plus release identity remains owned
by this repository: Git tag `v0.1.168+custom.001`, embedded version
`0.1.168+custom.001`, and OCI tag `v0.1.168-custom.001`. The upstream `VERSION`
value is not copied because it is not the Plus release identity.

## Migration identity

Plus already owns migration prefixes through `195`. The upstream Passkey SQL
is introduced as `196_passkey_credentials.sql` with equivalent idempotent DDL.
Existing migration files are never renamed or edited. The migration runner
identifies files by full filename and checksum, preserving upgrade safety for
existing Plus databases.

## Authentication and routing

WebAuthn deployment configuration is a security boundary, not an activation
signal. Passkey is available only when `WEBAUTHN_ENABLED`, RP ID, and origins
are valid and the database-backed administrator setting is explicitly true.
A missing setting is false, including databases upgraded from an earlier Plus
release. Container examples forward the same four WebAuthn variables.

Global IP access control continues to run after CORS and before every
authentication and application route. A successful Passkey assertion clears
the source-IP failure streak, records the successful login, and only then
issues the normal token pair. Failure to clear configured IP state is fail
closed. Assertion failures retain endpoint rate limits and do not join the
password/TOTP automatic blocking policy without a separate decision.

## Model Plaza exposure

The feature is disabled when no setting is stored. An enabled Plaza requires
authentication unless the administrator explicitly sets
`model_plaza_require_auth=false`. Exclusive-group visibility continues to use
the user's allowed-group relation and does not imply an active subscription.
When authentication is required, the global 401 flow sends the browser to a
login URL containing only its current internal path, query, and fragment so a
successful login can return to the Plaza without creating an open redirect.

## Gateway composition

The merge retains Plus OpenAI outbound identity, OAuth session isolation,
IP-sideband checks, five-hour quota, and stream-output observation. Upstream
Live store recovery is added without allowing an access-control change to
leave a sideband connection alive. Messages and Chat fallback observe the
converted downstream output so `first_token_ms`, `first_output_ms`, and
`first_output_kind` retain their current contracts.

## Prompt-audit key handling

Prompt-audit endpoint tokens may only be saved when a stable
`TOTP_ENCRYPTION_KEY` is configured. Ciphertext that cannot be decrypted stays
visible as invalid and is excluded from runtime use until an administrator
re-enters or clears it. Documentation distinguishes this requirement from
optional TOTP UI use.

## Rollout and rollback

Passkey tables are additive; application rollback is schema-safe, although
Passkey users must use an existing authentication method while rolled back.
Initial deployment leaves Passkey and Model Plaza disabled. The local upgrade
uses the normal Apple Containers `up` flow so dependency containers and named
volumes remain intact.
