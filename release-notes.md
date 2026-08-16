Sub2API Plus v0.1.176+custom.002

## Highlights

- Gate IP access control behind a system-settings master switch. Security
  Audit stays hidden and enforcement stays off unless
  `global_ip_access_control_enabled` is explicitly on.
- Make Codex version auto-sync observable and runnable on demand. Admin
  settings show the effective outbound version and its source, persist last
  check time and errors, and can sync from GitHub immediately.
- Estimate admin usage TPS from last-token timing, and keep out-of-band
  values visible as `< 1` or `> 1000`.
- Add admin usage user-agent copy, a trusted-proxy setup guide, and a
  backup-page guide for the fixed secret encryption key.
- Run official push and release checks only inside the platform validation
  container.

## Changed

- Default the purchase page to the subscription tab.
- Add missing Tencent Captcha region i18n labels.
- Show usage latency TPS with the shared `formatTpsDisplay` helper.

## Fixed

- Close GO-2026-6222 by bumping `golang.org/x/image`.
- Keep validation-container caches, pnpm, frontend overlays, and worktree
  gates aligned so push-cli and release-cli can finish on Apple Container
  and Docker.

## Compatibility and migration

- Database migration `221_add_usage_log_last_token_ms.sql` adds nullable
  `usage_logs.last_token_ms`. Historical rows stay NULL and do not show TPS.
- IP enforcement now requires both the new global master switch and the
  existing IP-page toggle. Upgrades keep the master switch off, so previous
  page-only enforcement does not resume until an administrator turns it on.
- Codex outbound identity source precedence is unchanged.

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
