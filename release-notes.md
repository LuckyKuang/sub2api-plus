Sub2API Plus v0.1.173+custom.004

## Highlights

- Add explicit Codex client-profile policies for enabled OpenAI OAuth accounts.
- Keep OAuth shared sessions isolated by account policy, user, group, and
  response ownership while preserving API-key continuation behavior.

## Fixed

- Reject cross-group OAuth shared-response continuations without bypassing the
  proxy stream quarantine fail-open safeguard.
- Keep Codex request profile enforcement consistent across Responses, Chat
  Completions, Anthropic-compatible endpoints, Alpha Search, Count Tokens, and
  WebSocket forwarding.
- Correct several frontend account-scheduling translations and administration
  layout details.

## Compatibility and migration

- No database migration is required.
- Existing OpenAI OAuth accounts retain their configured session-sharing policy.
  Administrators should review enabled client-profile restrictions before
  deployment because unmatched clients are denied locally.

## Known issues

- Client-profile matching validates request characteristics and cannot attest
  the originating client binary.
- Existing response chains that do not have a policy-scoped ownership record
  retain the established group-local continuation behavior.

## Upstream baseline

Official release: v0.1.173
Official commit: 29009f0b2ea14edf3b11ae2564fb617ff91a03b4
