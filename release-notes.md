Sub2API Plus v0.1.181+custom.001

## Highlights

- Import official Sub2API v0.1.179–v0.1.181: adaptive Chinese-provider protocols, channel Fast/Flex/context multipliers, Composite Codex/CN routing, Fast `service_tier`, reset-card auto-use, experimental OAuth transport plugins, Go 1.27.0, and the four 181 compatibility fixes.
- Keep Plus Prompt Audit conversation-text selection, admin user support view, IP access master switch, Codex mode-only fingerprint with `device` default, usage TPS, and release tooling.

## Changed

- Go toolchain is `1.27.0`; golangci-lint is `2.13.0`.
- Usage aggregation uses official GROUPING SETS; Plus TPS (`last_token_ms`) remains.
- Fast `service_tier` covers Responses, Chat, and WebSocket.
- Experimental OAuth transport plugins ship with API, admin UI, config, and docs, and stay disabled by default.
- Official migrations land as Plus `229` usage-log indexes, `230` Composite CN routes, `231` channel multipliers, `232` plugins, and `233` plugin artifacts.

## Fixed

- Gemini tool-schema cleanup, Grok official CLI User-Agent, Responses Lite `parallel_tool_calls`, and batch cleanup of unsupported `status` fields from official v0.1.181.

## Compatibility and migration

- **Breaking billing change from official v0.1.179:** long-context billing now activates when either the group switch or the account switch is enabled. Existing OpenAI traffic above 272k context may start paying 2× input / 1.5× output.
- Published Plus migrations 224/225/226/228 are unchanged. Do not reuse empty prefix `227`.
- Official `225_backfill_codex_fingerprint_seed.sql` is not imported. Codex identity remains mode-only and derived from the credential-owning account.
- Fresh databases and upgrades from `v0.1.178+custom.005` both apply `229`–`233`.

## Known issues

- OAuth transport plugins are experimental and default off.
- Frontend lockfile still needs a frozen `pnpm install` after this merge so plugin and `dompurify 3.4.14` dependencies are fully converged.

## Upstream baseline

Official release: v0.1.181
Official commit: 3af5443b224823ae507a50c7b113aa50604409c8
