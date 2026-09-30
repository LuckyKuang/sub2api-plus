# Upstream v0.2.10 integration

This source integration uses the official tag and commit recorded in
[UPSTREAM.md](../UPSTREAM.md). It layers `v0.2.10` onto the Plus tree that
already incorporated the official `v0.2.9` tag documented in
[v0.2.9 integration](UPSTREAM_V0_2_9_INTEGRATION.md). The merge-base of the
two trees is that already-integrated `v0.2.9` commit, so no official release
was skipped.

Unlike the previous integration, the Plus version was promoted **before** the
merge: `0.2.10+custom.001` lands as its own commit ahead of the tag import, so
this integration does not itself perform the version promotion. Back up the
database before any upgrade.

This increment ships no schema migrations.

## Official changes imported

- Claude Sonnet 5.5 and Opus 5.5: the effort catalog, `IsSonnet55`, the built-in
  fallback pricing cards, the 1,000,000-token context window, and the frontend
  model catalog entries with adaptive thinking and effort variants.
- Forwarded stream usage normalization for chat completions, Responses, and the
  Anthropic-native paths of the OpenAI gateway.
- Composite-group WebSocket routing and an account-model ownership veto in the
  account scheduler.
- Rate-limit / billing probes remain retired on the Plus tree; the official
  `UpstreamBillingProbeService` provider is not resurrected.
- Single-pass tool-name rewriting.
- A sanitized native Claude reset-credit status API plus the admin account cell
  that displays it.
- A cyber-policy user allowlist that degrades a hit to observability
  (`cyber_log_only` / `risk_control_log_only`) instead of blocking.
- Dashboard usage trend toggling between tokens and spending.
- Model-whitelist mapping conflict fixes in the account modals.
- Antigravity compatibility streams stay alive until the first content.
- The OpenAI gateway omits the Codex model catalog for OpenAI groups, and
  Claude Code-only groups expose just the Claude Code client tab.

## Plus-preserved behavior (integration decisions)

The owning provider, identity, audit and pricing documents and their regression
tests record the durable integration contracts. The main decisions are:

| Area | Integrated behavior |
| --- | --- |
| Version / release | `0.2.10+custom.001` was promoted in its own commit before the merge; the tag import itself does not publish, retag, or promote further. Tag/Release/image publication and the `published` mapping stay in the release-cli flow. |
| Pricing | Sonnet 5.5 / Opus 5.5 are additive. The GPT-6 Sol/Luna fallback cards, the `cache_creation_input_token_cost` field-presence rule (an explicit `0` stays `0`; the derived 1.25x rule applies only when the field is absent), the strict `>272000` long-context tier, and the `opus-5.5` exact family isolation are all preserved — the previous integration had dropped those cards once already. |
| Identity / beta | Credential-owner identity precedence and the terminal-send contract are unchanged. `filterSonnet55ToolsetBetaHeader` is applied *after* `ApplyAccountOutboundIdentity` and after the existing `anthropic-beta` final-decision logic, so it cannot bypass the drop policy, re-introduce a dropped beta, or alter the identity triple. |
| Ingress audit | The two-engine boundary is unchanged. Plus's `PromptTextAuthority`, `SessionID`, `AdminUser`, `Stage` and `BodyBytes` fields and its `shadow` / `cyber_policy` / `session_block` actions are kept. The upstream log-only modes are an *additional* dimension, and the moderation repository now applies both Plus's `action <>` filters and upstream's `mode <>` filters. |
| Extraction | Keyword scanning routes through Plus's canonical `auditcontent` extraction. Upstream's `filterReminders` variant has no analogue in the Plus extractor, so `extractContentModerationKeywordText` delegates to `ExtractContentModerationText` instead of introducing a second extraction path. |
| Settings | `cyber_policy_user_allowlist` is added alongside every Plus-only key: `global_ip_access_control_enabled`, `client_disconnect_consecutive_ban_*`, `async_image_user_images_per_minute`, `login_agreement_*`, and the affiliate/promo series. |
| Wire | `ipAccessControlHandler`, `ipAccessControlService` and `clientDisconnectRiskService` stay; `openCodeGoUsageService`, `claudeResetCreditService` and the `compositeRouteResolver` argument are added. The retired upstream billing probe is not resurrected. |
| Scheduling | The composite account-model ownership veto is layered *on top of* Plus's OAuth session-group veto; neither replaces the other. |
| Streaming | Plus's `responseCommitted`, real terminal detection, audio metering, partial-usage handling and output timing remain authoritative. The mapped model is written back before the Responses → Anthropic conversion so protocol selection sees the real upstream model, and the tool-input seed/delta de-duplication fix from the v0.2.9 integration is not regressed by the single-pass tool rewrite. |

## Upgrade notes

- `claude-sonnet-5-5` and `claude-opus-5-5` are now recognized end to end, with a
  1,000,000-token context window and adaptive thinking defaults (Sonnet 5.5
  defaults to `high` effort).
- The admin cyber-risk center gains a user allowlist. Content Moderation and
  cyber-risk hits keep evidence without local blocking or ban counts; Prompt
  Audit retains its independent policy.
- The admin dashboard can switch the recent-usage trend between tokens and
  spending.
- The account modal shows the Claude native reset-credit status on demand.
  Queries preserve account identity precedence and the token-refresh snapshot;
  see [Claude provider behavior](providers/CLAUDE.md).

## Validation boundary

All generation and validation runs with the pinned repository toolchain in
Apple Containers on macOS, Docker inside WSL2 Debian/Ubuntu on Windows, or
Docker on Linux. Host-side validation is forbidden.

The integration checks cover pricing and usage, identity source priority and
transport declarations, canonical extraction and HTTP/WS audit ordering,
allowlist side effects, composite routing, frontend components and locales.
The maintained Ent/Wire generation entrypoint is `make -C backend generate`;
the validation image includes GNU Make for this purpose.

The complete local matrix uses
`python3 skills/push-cli/scripts/push_cli.py check --base-ref origin/main --serial`.
Run the repository and Prompt Audit PostgreSQL integration tests separately
with explicit test DSNs and an isolated Redis service: the ordinary matrix
container has no Docker socket and its testcontainers-dependent cases may
skip. Use separate disposable test databases and remove their containers,
network and writable data afterwards. See [Contributing](../CONTRIBUTING.md)
for the mandatory environment, toolchain and cleanup requirements.
