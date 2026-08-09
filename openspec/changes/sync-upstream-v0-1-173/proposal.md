## Why

Official Sub2API v0.1.173 adds Grok/xAI integration, channel monitor V2,
email-domain registration limits, provider pricing, and gateway corrections.
The same areas contain intentional Plus migrations, quota behavior, Codex
identity rules, administration UI, and release metadata, so the upstream
baseline must be integrated and verified as a single cross-cutting change.

## What Changes

- Merge official v0.1.173 commit
  29009f0b2ea14edf3b11ae2564fb617ff91a03b4 while preserving documented Plus
  behavior.
- Import official migrations as unique Plus prefixes 202 through 218 without
  rewriting any migration published through v0.1.172+custom.001.
- Keep channel monitor V1 as the safe default and require explicit opt-in for
  V2 passive monitoring.
- Keep Grok cross-client model mapping disabled unless the stored setting is
  exactly `true`, and keep password-based Grok OAuth hard-disabled.
- Integrate Gemini pool-mode rate-limit handling, response-based image output
  accounting, and Antigravity response-model observation corrections.
- Restore merge-sensitive Plus gateway invariants for OAuth session sharing,
  partial stream accounting, Grok endpoint selection, and all OpenAI token
  classes.
- Restore the Plus frontend lock graph lost during conflict resolution so it
  remains synchronized with the retained Vite, Vitest, DOMPurify, and security
  override declarations.
- Prepare v0.1.173+custom.001 release metadata without creating a tag,
  GitHub Release, image, or remote push.

## Impact

- Persistent data: seventeen forward-only migrations are introduced under
  Plus-owned prefixes.
- Public and administrative behavior: Grok, channel monitor, registration
  policy, provider pricing, and settings contracts change.
- Security boundary: password-based Grok OAuth remains unavailable regardless
  of the retained compatibility setting.
- Billing and scheduling: Gemini image output counting, pool-mode error
  handling, partial stream usage, and audio output aggregation are corrected.
- Operations: deployment examples, release documentation, and upstream mapping
  move to v0.1.173+custom.001; frozen frontend installation remains
  reproducible from the preserved Plus lockfile.
