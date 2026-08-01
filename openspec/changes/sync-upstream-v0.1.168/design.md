## Baseline and release identity

The official baseline is `v0.1.168` at
`99c8e4bf7564823bafbab369acab6539e734c1bb`. Plus release identity is always
owned by the Plus repository: Git tag `v0.1.168+custom.001`, embedded version
`0.1.168+custom.001`, and OCI tag `v0.1.168-custom.001`. The upstream source
VERSION value is not copied because it is not the Plus release identity.

## Migration identity

Plus already owns migration prefixes through `195`. The upstream Passkey SQL
is introduced as `196_passkey_credentials.sql`, with identical idempotent DDL.
Existing files are never renamed or edited. The migration runner identifies
files by their full filename and checksum, so this preserves upgrade safety for
all existing Plus databases.

## Authentication and routing

Global IP access control continues to run after CORS and before every
authentication and application route. Optional JWT authentication for Model
Plaza therefore cannot bypass an IP block.

Passkey login uses the same successful-authentication policy as password,
TOTP, and OAuth: clear the source-IP failure streak, then record the successful
login, then issue the normal token pair. The clear operation remains fail
closed when the configured IP access service is unavailable. Passkey assertion
failures retain the upstream endpoint rate limits and do not join password/TOTP
automatic blocking without a separate product decision.

## Model Plaza exposure

The feature is disabled when no setting is stored. An enabled Plaza requires
authentication unless the administrator explicitly sets `model_plaza_require_auth`
to false. Its existing visibility model is intentionally a showcase: exclusive
groups use the user's allowed-group relation and do not imply an active
subscription. The UI must make anonymous exposure an explicit administrative
choice.

## Gateway composition

The merge retains Plus OpenAI outbound identity, OAuth session isolation,
IP-sideband checks, five-hour quota, and stream-output observation. Upstream
Live store recovery is added without allowing an access-control change to leave
a sideband connection alive. Changes to Messages and Chat fallback must observe
the converted downstream output so `first_token_ms`, `first_output_ms`, and
`first_output_kind` keep their current contracts.

## Prompt-audit key handling

Prompt-audit endpoint tokens may only be saved when a stable
`TOTP_ENCRYPTION_KEY` is configured. Existing ciphertext that cannot be
decrypted remains visible as invalid but is excluded from runtime use until an
administrator re-enters or clears it. Documentation must distinguish this
requirement from optional TOTP UI use.

## Rollout and rollback

Passkey tables are additive; an application binary rollback is safe at the
schema level, though Passkey users fall back to existing authentication while
rolled back. Initial deployment leaves Passkey and Model Plaza disabled. The
release is deployed through normal Apple Containers `up` so dependency
containers and named volumes remain intact.
