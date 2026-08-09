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
- Grok cross-client model mapping is off by default (strict opt-in; missing key
  does not enable silent gpt/claude→grok rewrite). Grok password login stays
  hard-disabled and hidden from create/reauth UI; the compatibility config flag
  is ignored and cannot re-enable the server path.
- OpenAI/Codex gateway keeps Plus identity resolution while adopting official
  routing hints, legacy Responses beta stripping for OAuth, and Grok native
  search surcharge counting.
- OAuth session-sharing policy is enforced consistently across ordinary HTTP,
  passthrough HTTP, and Responses WebSocket forwarding; interrupted OpenAI and
  Grok streams retain observed usage and first-output diagnostics.
- Grok Anthropic Messages forwarding honors the global endpoint mode on the
  initial request and encrypted-content retry. OpenAI usage aggregation retains
  audio output tokens, and Gemini skipped/temp-unscheduled policy outcomes no
  longer fall through to default account-state mutation.
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

- No known release blockers were found by the full backend unit/integration,
  frontend, lint, migration, documentation, identity, and release-policy checks.
- GitHub Release and OCI publication still require the separate maintainer
  approval workflow.

## Upstream baseline

Official release: v0.1.173
Official commit: 29009f0b2ea14edf3b11ae2564fb617ff91a03b4
