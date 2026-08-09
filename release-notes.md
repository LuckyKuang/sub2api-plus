Sub2API Plus v0.1.173+custom.001

## Highlights

- Sync official v0.1.173: full Grok/xAI integration, channel monitor V2 (passive),
  email-domain registration limits, Grok media/voice/search pricing, and related
  gateway fixes.
- Preserve Sub2API Plus Codex outbound identity precedence, five-hour subscription
  quota, local quota display, async image history, IP access control, and
  distribution naming.

## Changed

- Import official channel-monitor V2 and Grok pricing migrations as Plus
  forward-only files `202`–`218` (official content renumbered; no rewrite of
  published Plus `187`–`201`).
- Channel monitor mode defaults remain fail-safe for active probes (`v1`) when
  unset; V2 is explicit opt-in (see `docs/channel-monitor-v2-safe-defaults.md`).
- Grok cross-client model mapping is off by default; Grok password login remains
  hard-disabled server-side.
- OpenAI/Codex gateway keeps Plus identity resolution while adopting official
  routing hints, legacy Responses beta stripping for OAuth, and Grok native
  search surcharge counting.
- Auth-cache snapshot version includes Plus five-hour/profit fields plus Grok
  search/audio/video model price fields.

## Compatibility and migration

- Apply database migrations `202`–`218` on upgrade from `0.1.172+custom.001`.
- Migration `218` clears non-Grok (non-composite) group video generation config
  after writing `groups_video_price_backup_218`.
- Operators that relied on implicit `gpt-*` / `claude-*` → Grok rewriting must
  enable the Grok cross-client mapping setting.
- Email domain registration quota is off by default.

## Known issues

- Full frontend `pnpm install` / vitest suite for channel-monitor-v2 should be
  run before publication.
- GitHub Release and OCI publication require the separate maintainer approval
  workflow.

## Upstream baseline

Official release: v0.1.173
Official commit: 29009f0b2ea14edf3b11ae2564fb617ff91a03b4
