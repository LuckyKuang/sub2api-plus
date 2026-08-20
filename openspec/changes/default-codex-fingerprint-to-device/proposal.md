## Why

Codex fingerprint convergence currently treats an absent
`codex_fingerprint_mode` as `session`. That makes persisted account behavior
depend on the application version's implicit default and prevents exports,
audits, and synchronization paths from showing the administrator's effective
choice.

The safer default is device-only convergence: one account-level installation
identity while each client retains its own session and thread boundaries.

## What Changes

- Make `device` the default Codex fingerprint convergence mode for real OpenAI
  OAuth credential accounts.
- Persist every selected mode explicitly, including `device` and `session`.
- Backfill missing, empty, null, or invalid stored modes to `device` and enforce
  an explicit valid value for future writes.
- Keep runtime reads defensive by treating malformed legacy data as `device`
  without writing from request-forwarding paths.
- Align create, edit, bulk edit, Codex import/PAT, CRS synchronization, locale
  text, and tests with the new invariant.

## Compatibility

Existing real OpenAI OAuth accounts without a valid explicit mode currently
behave as `session`; the migration intentionally changes them to `device`.
Existing explicit `off`, `device`, `session`, and `full` choices are preserved.
API-key, setup-token, and credential-shadow accounts remain outside this
setting.

## Impact

- Affected capability: `codex-fingerprint-convergence`.
- Affected data: `accounts.extra.codex_fingerprint_mode`.
- Affected code: account persistence and synchronization, Codex outbound
  fingerprint resolution, account administration UI, locales, and tests.
