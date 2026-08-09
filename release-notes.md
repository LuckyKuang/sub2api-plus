Sub2API Plus v0.1.172+custom.001

## Highlights

- Sync the official v0.1.172 OAuth security, upstream model-audit, Codex
  identity, quota-window, transport, failover, billing, and CAPTCHA updates.
- Preserve Sub2API Plus outbound identity resolution, five-hour subscription
  quota behavior, local quota display, deployment behavior, and distribution
  naming.

## Changed

- Reject pending OAuth completion when the existing email identity belongs to a
  different account, preventing account takeover.
- Record the upstream response model and model-mismatch state for usage logs;
  add admin filtering, display, and spreadsheet export columns.
- Add forward-only migrations 200 and 201 for the new usage-log audit fields.
- Make `codex-tui/0.147.0 (Ubuntu 24.04; x86_64) xterm-256color` the default
  outbound Codex User-Agent, paired with Originator `codex-tui` and Version
  `0.147.0`, while enforcing the single resolver's valid account User-Agent >
  valid global User-Agent > compiled default source precedence. Version
  synchronization can update only the selected identity's version declarations.
- Reset daily subscription quota windows at midnight in the configured
  timezone, while periodic and five-hour windows retain their existing anchor.
- Include official transport timeouts, pre-output failover behavior, capacity
  error handling, billing quantization, Tencent CAPTCHA region support, Grok
  handling, and Responses-to-Anthropic compatibility corrections.

## Compatibility and migration

- Apply database migrations 200 and 201 before relying on upstream response
  model audit filters or exports.
- Existing account-specific and global Codex User-Agent settings remain
  supported. Invalid or missing account values fall through to the global
  setting, and the standard Codex TUI identity is used only when neither source
  is valid. Automatic versions older than `0.147.0` no longer override the
  compiled baseline; an upstream-supported explicit administrator version
  remains authoritative.
- Deployments that require the exact new default fingerprint must clear older
  account/global User-Agent overrides and any explicit administrator version
  override. Automatic synchronized values require no manual cleanup.

## Known issues

- CAPTCHA provider credentials and client IDs must be configured before their
  corresponding authentication gates can be enabled.
- GitHub Release publication and OCI image publication require the separate
  maintainer approval workflow.

## Upstream baseline

Official release: v0.1.172
Official commit: 155c494964c3ea6ecc31f52679525c1034bf0f16
