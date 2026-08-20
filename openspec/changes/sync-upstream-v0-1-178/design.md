## Baseline

| Item | Value |
| --- | --- |
| Plus HEAD | `e7d48639de9fe0c46f788dfaf3cec2872618ce12` (latest `origin/main`) |
| Current official baseline | `v0.1.177` / `073e92d17178a1ccdb0a27017f572f10c9c7ab62` |
| Merge input | `v0.1.178` / `e0c48a19ed794a565e3858662520afe0a1f9f0ba` |
| Prepared Plus version | `v0.1.178+custom.001` |

Only the annotated `v0.1.178` tag is merged. `upstream/main` and later commits
are outside this change.

## Ownership

| Area | Owner | Merge rule |
| --- | --- | --- |
| Module path, workflows, release metadata, `UPSTREAM.md` | Plus | Keep the Plus repository identity and mark the new mapping planned. |
| Channel-monitor quota and CN-provider services | Official plus Plus adapters | Import the complete schema, repository, service, route, UI, and locale lifecycle. |
| Channel time pricing | Official | Import the recurring-period schema and pricing evaluation without changing existing migrations. |
| Codex fingerprint mode | Plus | Keep the latest Plus mode-only implementation and its explicit `device` default; do not reintroduce upstream seed persistence. |
| Responses/WS forwarding | Composed | Adopt binary-safe turn handling and timing while preserving Plus prompt-cache, compaction, and session-policy guards. |
| Generated Ent/Wire output | Generated | Regenerate from schemas and providers; do not hand-edit generated files. |

## Migration ordering

The latest Plus baseline owns `224_backfill_codex_fingerprint_mode.sql`.
Official channel pricing and monitor migrations remain `225` and `226`; the
CN-provider quota constraint is installed as `228_user_platform_quotas...sql`
to keep new prefixes unique and increasing. The upstream seed backfill is not
carried forward because it conflicts with the Plus mode-only contract.

## Identity and routing

HTTP and WebSocket builders use the latest Plus request-scoped mode-only
fingerprint state and account-level identity rules. Compact requests keep their
raw compact identity and skip ordinary body-cache rewriting. OAuth Messages
compatibility preserves an explicit empty `instructions` field when the client
omitted instructions, preventing an inferred developer guard from changing the
request contract.

The outbound identity source order remains valid credential-owner
`credentials.user_agent`, valid global `openai_codex_user_agent`, then the
compiled default. Fingerprint changes cannot select a different client family,
Originator, or session-policy result.

## Verification strategy

Run migration and generated-code checks, backend service/handler/repository
tests, frontend lint/typecheck/Vitest, release-document checks, and diff/import
policy checks. The PR remains reviewable and unpublished; release promotion is a
separate maintainer action.
