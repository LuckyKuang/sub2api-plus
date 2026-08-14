Sub2API Plus v0.1.176+custom.001

## Highlights

- Import official Sub2API v0.1.176 while keeping Plus Codex client-profile
  enforcement, OAuth session isolation, and the outbound identity triple.
- Add official Codex fingerprint convergence. Unset OpenAI OAuth accounts
  use `session` mode: one upstream device and session, with threads derived
  from the client-original session-id.
- Add Grok 4.6, JWT subscription-tier detection, per-group model pricing,
  `/x_search`, and upstream response-model billing.

## Changed

- Official OpenAI failover and accounting fixes: HTML 403 is not an account
  penalty, empty `response.completed` fail over, deterministic 400 stays
  non-retryable, and visible TTFT ignores terminal events.
- Group pricing now includes official `model_pricing` and
  `long_context_pricing_enabled` beside Plus five-hour, live, and
  profit-control fields.

## Compatibility and migration

- Database migration `220_group_model_pricing.sql` adds
  `groups.long_context_pricing_enabled` (default true) and
  `groups.model_pricing`.
- Existing OpenAI OAuth accounts without `codex_fingerprint_mode` start
  using official `session` convergence. Store `off` to keep the previous
  outbound device/session visibility.
- Client-profile matching, OAuth session access policy, and
  account > global > default User-Agent precedence are unchanged.
- Usage logs still persist the sanitized client-original session-id.

## Known issues

- Client-profile matching validates request characteristics and cannot
  attest the originating client binary.
- Fingerprint convergence changes only outbound Codex installation,
  session, and thread carriers. It does not prove a single physical device.
- Official post-176 Grok long-context and media-fallback fixes are not
  included; the merge input is tag v0.1.176 only.

## Upstream baseline

Official release: v0.1.176
Official commit: e803e3851c0a7e222cfadeafad7b8636ab959d11
