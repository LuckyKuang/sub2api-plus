## Decision

`codex_fingerprint_mode` is a required persisted property of every real OpenAI
OAuth credential account. Its canonical values are `off`, `device`, `session`,
and `full`; `device` is the creation default.

Application persistence normalizes account objects before repository writes so
API responses and exports immediately expose the canonical value. A database
trigger enforces the same invariant for CRS, mixed-version, and direct SQL
writes. Runtime forwarding retains a pure-read `device` fallback for malformed
legacy state and never repairs data on the request hot path.

## Write Semantics

- Inserts with a missing, null, or empty mode store `device`.
- Valid strings are trimmed and stored explicitly.
- For real OpenAI OAuth accounts, unknown strings and non-string values are
  rejected by normal application writes.
- An update that omits the key preserves a valid existing value. If neither the
  new nor old row has a valid value, it stores `device`.
- Frontend create, edit, and enabled bulk controls always submit the selected
  value. They no longer use deletion or JSON null to represent a default.

## Migration

Migration `224_backfill_codex_fingerprint_mode.sql` canonicalizes all real
OpenAI OAuth credential accounts. Missing, null, empty, and invalid modes become
`device`; valid modes are trimmed and preserved. The migration removes the
inapplicable key from Spark credential shadows and non-OAuth accounts.

## Alternatives Rejected

Keeping an absent key as the default representation saves a few JSON bytes but
continues coupling account behavior to code-version defaults. Repairing missing
values during request forwarding would add database side effects and cache
coordination to a high-frequency path. Frontend-only normalization would miss
Codex imports, PAT creation, CRS synchronization, and direct API writers.
