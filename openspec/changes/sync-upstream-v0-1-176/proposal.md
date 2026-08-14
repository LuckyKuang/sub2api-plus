## Why

Official Sub2API has moved from v0.1.173 to v0.1.176. The 175 line
adds Codex OAuth fingerprint convergence, upstream response-model
billing, backup volume uploads, and a large set of OpenAI failover and
accounting fixes. The 176 line completes the Grok stack already imported
in Plus: grok-4.6, JWT subscription tiers, per-group model pricing,
`/x_search`, and several billing/cache corrections.

Plus is published at `v0.1.173+custom.004` and owns Codex client-profile
enforcement, OAuth session isolation, usage session persistence, five-hour
quotas, and the custom release identity on the same files. A mechanical
merge is unsafe. This change records the reviewable merge assessment
before any code is imported.

## What Changes

- Adopt official tag `v0.1.176` commit
  `e803e3851c0a7e222cfadeafad7b8636ab959d11` as the next Plus baseline.
- Merge that tag only. Do not merge `upstream/main` and do not
  cherry-pick the three post-176 commits.
- Import official migration `221_group_model_pricing.sql` as Plus
  `220_group_model_pricing.sql`. Published Plus migrations through `219`
  remain immutable.
- Keep Plus authoritative for Codex identity precedence, client-profile
  enforcement, OAuth session access policy, usage-log session persistence,
  five-hour quotas, Grok safety defaults, channel-monitor V1 default,
  frontend lockfile, and distribution metadata.
- Keep official authoritative for fingerprint convergence, response-model
  billing, Grok 4.6 / JWT tier / x_search, and group `model_pricing` /
  `long_context_pricing_enabled`.
- Compose fingerprint rewrite after Plus session-policy resolution. Do
  not let account-constant fingerprint IDs replace usage-log correlation
  or bypass session access checks. Unset OAuth accounts follow official
  `session` mode.
- Prepare `v0.1.176+custom.001` metadata only after the merge is
  implemented. This assessment does not create a tag, Release, image, or
  remote push.

## Impact

- Persistent data: one new group-pricing migration under Plus prefix 220.
  Existing OAuth accounts with no `codex_fingerprint_mode` will start
  using official default `session` convergence unless reviewers choose a
  different default.
- Security boundary: client-profile and session-policy fail-closed
  behavior stay in force; fingerprint rewrite is outbound-only.
- Billing and scheduling: response-model billing, group model pricing,
  Grok 4.6 / x_search pricing, HTML 403 handling, and several OpenAI
  failover paths change.
- Administration UI: account fingerprint mode, group model pricing, usage
  request-id visibility, and Grok tier badges must land beside existing
  Plus session-policy and usage session-id surfaces.
- Operations: release mapping moves to official v0.1.176 /
  `0.1.176+custom.001` only after implementation.
