Sub2API Plus v0.1.178+custom.002

## Highlights

- Harden the shared security-audit content-extraction contract so Content
  Moderation and Prompt Audit consume the same canonical protocol document.
- Fail closed on incomplete or partial extraction whenever a blocking audit
  mode is active, including sibling content that would otherwise hide a failed
  field.
- Surface extraction metrics in risk-control and prompt-audit runtimes, and
  document the protocol matrix in `docs/SECURITY_AUDIT_CONTENT_COVERAGE.md`.

## Changed

- Route every content-bearing HTTP request, WebSocket turn, and Live Sideband
  client frame through `backend/internal/auditcontent` after authentication
  and before account selection, billing, concurrency, routing, or upstream
  writes.
- Keep the official v0.1.178 baseline and Plus customizations; this iteration
  does not change the embedded Codex identity precedence.

## Fixed

- Treat a partial extraction hidden by successful sibling content as an
  extraction failure, not a successful or empty request.
- Satisfy staticcheck tagged-switch lint in audit extraction role handling.

## Compatibility and migration

- None. Existing data remains compatible and this iteration adds no database
  migrations.

## Known issues

- None known.

## Upstream baseline

Official release: v0.1.178
Official commit: e0c48a19ed794a565e3858662520afe0a1f9f0ba
