Sub2API Plus v0.1.178+custom.001

## Highlights

- Import official v0.1.178 channel-monitor quota modes, Chinese-provider
  balance/quota support, and time-based channel pricing.
- Preserve the latest Plus Codex mode-only behavior, including the explicit
  `device` default, prompt-cache, compaction, and WebSocket relay safeguards,
  while adopting the upstream protocol fixes.
- Add the upstream account, scheduler, billing, and platform UI updates with
  synchronized English and Chinese locales.

## Changed

- Add forward-only migrations for platform quotas, the Plus Codex mode backfill,
  channel time pricing, and monitor quota configuration.
- Keep the custom Go module identity and Plus outbound identity precedence
  while importing the official v0.1.178 baseline.

## Compatibility and migration

- Existing data remains compatible. Startup applies the new migrations in
  lexical order: 224, 225, 226, and 228; no manual migration is required.
- The release is prepared as `planned` and is not published yet. Do not use
  its image tag until the release process completes.

## Known issues

- The fork PR still requires maintainer review and required GitHub checks
  before it can be merged into `main`.

## Upstream baseline

Official release: v0.1.178
Official commit: e0c48a19ed794a565e3858662520afe0a1f9f0ba
