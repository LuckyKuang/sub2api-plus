## 1. Contract

- [x] 1.1 Document IP-only failure ownership, 401/403/429/503 contracts and reset semantics.
- [x] 1.2 Replace the ambiguous failure-state API model with layered rule/enforcement fields.

## 2. Authentication semantics

- [x] 2.1 Stop successful password, TOTP, OAuth and Passkey authentication from clearing IP failure state.
- [x] 2.2 Count credential-level Passkey failures without counting infrastructure failures.
- [x] 2.3 Record explicit audit outcomes for recorded, blocked and unavailable risk-control results.
- [x] 2.4 Confirm an active auto block after ambiguous transaction commit errors before returning 403.

## 3. Request-path snapshot

- [x] 3.1 Add singleflight-protected complete snapshot refresh and startup warmup.
- [x] 3.2 Move refreshes to background event/periodic workers so requests do not refresh PostgreSQL.
- [x] 3.3 Apply committed local rule mutations directly and enforce per-rule expiration.
- [x] 3.4 Add maximum-staleness readiness/fail-closed coverage.

## 4. Management UI

- [x] 4.1 Render layered failure/block status and threshold context.
- [x] 4.2 Add manual refresh, last-updated time and visible-page auto refresh.
- [x] 4.3 Synchronize English and Chinese locale keys and update component tests.
- [x] 4.4 Add a dedicated, step-up-protected failure-state manual-block action with idempotent backend semantics.
- [x] 4.5 Add row-level confirmation, disabled/alternate states, duplicate-submit protection and post-action refresh.

## 5. Verification

- [x] 5.1 Run focused repository, service, handler and middleware Go tests.
- [x] 5.2 Run frontend lint, typecheck and focused Vitest coverage.
- [x] 5.3 Validate this OpenSpec change strictly and review the final diff.
- [x] 5.4 Re-run focused backend/frontend checks and strict validation for the manual-block extension.
- [x] 5.5 Re-audit Passkey failure classification, allow/emergency coverage, overlapping refreshes and manual-duration visibility; add regression coverage.
- [x] 5.6 Prevent stale snapshot overwrite, late release regression, TOTP-session miscounting, stale manual-block submissions and per-failure cleanup scans; add regression coverage.
