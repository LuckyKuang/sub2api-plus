## 1. Release and request safety

- [x] 1.1 Synchronize the unpublished `v0.1.173+custom.002` release metadata
  without creating or moving a tag, Release, image, or remote state.
- [x] 1.2 Add the Responses input-item identifier matrix and coverage for
  `custom_tool_call → ctc` preservation and mismatched-ID stripping.
- [x] 1.3 Convert the exact retry-buffer-limit `507` response to a
  non-retryable request-scoped `413`, with no account side effects.
- [x] 1.4 Verify the OAuth session-policy selection path continues to preserve
  non-policy selection failures.

## 2. Routing and upstream diagnostics

- [x] 2.1 Add typed scheduler diagnostics and propagate them to sanitized Ops
  error inputs.
- [x] 2.2 Add migration 219 and repository/API support for routing diagnostics
  and an independent routing-capacity marker, preserving existing SLA data.
- [x] 2.3 Classify connection transport failures and gateway timeouts, avoiding
  false model/account cooldowns while recording safe triage data.
- [x] 2.4 Add selected outbound-identity source diagnostics without changing
  identity precedence or fingerprints.

## 3. Administration and verification

- [x] 3.1 Use backend `time_range=24h` for the exact default rolling window
  and retain explicit boundaries for custom selections.
- [x] 3.2 Add explicit Error/Excluded/All Ops views and render sanitized
  routing details in the error detail UI.
- [x] 3.3 Add backend and frontend regression coverage; run formatting,
  relevant Go tests, frontend lint/typecheck/Vitest, migration checks, release
  checks, strict OpenSpec validation, and `git diff --check`.
