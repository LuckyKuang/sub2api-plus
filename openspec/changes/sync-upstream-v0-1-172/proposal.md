## Why

Official Sub2API v0.1.172 fixes a high-severity pending-OAuth account-takeover
path and introduces upstream response-model auditing. It also corrects Codex
identity defaults, subscription daily-window semantics, gateway failover,
billing precision, captcha interoperability, and upstream transport behavior.
Sub2API Plus has intentional module, release, outbound-identity, quota, and
usage-observability changes on the same paths, so a mechanical merge is unsafe.

## What Changes

- Merge official v0.1.172 commit 155c494964c3ea6ecc31f52679525c1034bf0f16
  while preserving intentional Plus behavior.
- Apply the OAuth pending-exchange security guard and its regression coverage.
- Add upstream response-model audit data, administration views, and filtering.
- Import the two upstream usage-log migrations as local prefixes 200 and 201;
  existing Plus migrations through 199 remain immutable.
- Move the default Codex identity to
  `codex-tui/0.147.0 (Ubuntu 24.04; x86_64) xterm-256color` while retaining
  and formally enforcing Plus's immutable valid account UA > valid global UA >
  compiled default source precedence. Version synchronization may update only
  the selected identity's version declarations.
- Align subscription daily quotas to the configured timezone midnight without
  regressing the Plus five-hour quota, atomic reset, or cache invalidation flow.
- Release the resolved baseline as v0.1.172+custom.001. No tag, release, image,
  or remote push is created by this change.

## Impact

- Security: OAuth identity binding, captcha browser policy, and outbound client
  identity behavior change.
- Persistent data: two forward-only usage-log migrations are added.
- APIs and UI: usage-log audit fields, filters, exports, subscription reset
  semantics, and Codex-related settings change.
- Operations: deployment CSP examples, transport timeout behavior, and version
  and upstream release metadata are synchronized.
