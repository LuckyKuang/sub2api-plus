## Baseline

| Item | Value |
| --- | --- |
| Plus HEAD | latest `origin/main` (`v0.1.178+custom.005`) |
| Current official baseline | `v0.1.178` / `e0c48a19ed794a565e3858662520afe0a1f9f0ba` |
| Merge input | annotated tag `v0.1.181` / `3af5443b224823ae507a50c7b113aa50604409c8` |
| Prepared Plus version | `v0.1.181+custom.001` |
| Go toolchain | official `1.27.0` |
| golangci-lint | `2.13` in `.tool-versions` and verification containers |

Only the annotated `v0.1.181` tag is merged. `upstream/main` and later commits
are outside this change. Do not squash official history or cherry-pick the
179/180/181 series.

## Ownership

| Area | Owner | Merge rule |
| --- | --- | --- |
| Module path, brand, workflows, release metadata, `UPSTREAM.md` | Plus | Keep Plus identity. Mark the new mapping `planned`. |
| Go version, plugin API/proto/routes/UI/docs | Official plus Plus adapters | Take official 1.27 and the complete plugin unit. Keep Plus module path. |
| Frontend Node/pnpm, Vite/Vitest, security overrides | Plus plus official deps | Do not roll back Plus toolchain. Absorb official plugin deps and `dompurify 3.4.14`. |
| Channel multipliers, adaptive protocols, Composite Codex/CN | Official | Import schema, services, routes, UI, locale, tests. |
| Usage aggregation | Composed | Take official GROUPING SETS; keep Plus TPS (`last_token_ms`) column and UI. |
| Fast `service_tier`, billing, long-context gating | Official plus Plus adapters | Official token/tier path is canonical. Keep Plus TPS, usage dedup, and identity accounting. |
| OpenAI/WS/HTTP bridge, Grok, Gemini, Responses Lite | Composed | Official failover/bridge/181 fixes as base; restore Plus session affinity, cache identity, fingerprint, and audit order. |
| `auditcontent` and security audit | Composed | Keep Plus extraction contract and fail-open boundaries. Import official config-store/reload fixes. Never take the whole directory as `ours`. |
| Codex fingerprint | Plus | Mode-only, default `device`, identity from credential-owning account ID. Exclude official seed lifecycle, SQL, tests, and any compensation migration. |
| Admin user support view, IP master switch, S3 date path, image-task cleanup, version-badge hiding, Codex version observability | Plus | Keep. |
| Published migrations 221/224/225/226/228 | Plus | Immutable filenames and checksums. |
| Security scan exceptions | Plus | Do not inherit official `nanoid` waiver. Fix expired Plus `lodash`/`lodash-es`/`axios` waivers before the PR. |

## Migration ordering

Current Plus max prefix is `228`. Empty prefix `227` must not be reused.

| Official file | Plus file |
| --- | --- |
| `226_add_usage_log_effective_model_indexes_notx.sql` | `229_add_usage_log_effective_model_indexes_notx.sql` |
| `227_composite_routes_add_cn_providers.sql` | `230_composite_routes_add_cn_providers.sql` |
| `228_channel_pricing_multipliers.sql` | `231_channel_pricing_multipliers.sql` |
| `229_plugins.sql` | `232_plugins.sql` |
| `230_plugin_artifacts.sql` | `233_plugin_artifacts.sql` |

Official `224_user_platform_quotas_add_cn_providers.sql` is already Plus `228`.
Official `225_channel_model_time_pricing.sql` and
`226_channel_monitor_quota_mode.sql` already exist on Plus. Official
`225_backfill_codex_fingerprint_seed.sql` stays deleted.

Update every test that reads these filenames through `FS.ReadFile(...)`.

## Identity, audit, and billing

HTTP and WebSocket builders keep Plus request-scoped mode-only fingerprint
state. User-Agent source order remains valid credential-owner
`credentials.user_agent`, valid global `openai_codex_user_agent`, then compiled
default. Fast `service_tier` wiring follows official Responses/Chat/WS paths
without changing that identity order.

Security audit remains after auth/basic validation and before account
selection, billing, concurrency acquisition, or upstream writes. Prompt Audit
continues to extract conversation text, not tool-schema dumps.

Long-context billing uses the official “any enabled switch” gate. Release notes
must say existing OpenAI traffic above 272k context may start paying 2× input /
1.5× output.

## Dependencies and scan exceptions

- `go.mod`: toolchain `1.27.0`, module path Plus, official plugin modules
  included, then `go mod tidy`.
- Frontend: keep Plus Node/pnpm, Vite 6, Vitest 3, and security overrides;
  add official plugin packages and `dompurify 3.4.14`; regenerate the lockfile.
- Do not copy official `nanoid` high-severity exceptions. Expired Plus lodash
  and axios exceptions must be upgraded, removed, or re-audited with a real
  owner, expiry, and rationale.

## Verification strategy

Run migration and generated-code checks, backend service/handler/repository
tests focused on OpenAI/Grok/Gemini/billing/audit/plugins, frontend
lint/typecheck/Vitest, locale parity, release-document checks, and
diff/import policy checks. Full `push-cli submit-pr` matrix runs in WSL2
Debian Docker, never host Docker. This change does not publish.
