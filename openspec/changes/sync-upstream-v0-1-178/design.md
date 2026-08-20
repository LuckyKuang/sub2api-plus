## Baseline

| Item | Value |
| --- | --- |
| Plus HEAD | `9cb7e1228d682aad7843fc430b01e505f657996d` (`v0.1.177+custom.002`) |
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
| Codex fingerprint seed lifecycle | Official plus Plus identity rules | Persist valid seeds, but keep account > global > compiled identity precedence. |
| Responses/WS forwarding | Composed | Adopt binary-safe turn handling and timing while preserving Plus prompt-cache, compaction, and session-policy guards. |
| Generated Ent/Wire output | Generated | Regenerate from schemas and providers; do not hand-edit generated files. |

## Migration ordering

The imported tree contains both `225_backfill_codex_fingerprint_seed.sql` and
`225_channel_model_time_pricing.sql`. Since the Plus migration policy rejects
new duplicate numeric prefixes, the seed backfill is installed as
`227_backfill_codex_fingerprint_seed.sql`; its SQL and checksum semantics are
unchanged. The official pricing and monitor migrations remain `225` and `226`.

## Identity and routing

HTTP and WebSocket builders stage one credential-owner fingerprint set and
apply the final prompt-cache/session identity afterward. Compact requests keep
their raw compact identity and skip ordinary body-cache rewriting. OAuth
Messages compatibility preserves an explicit empty `instructions` field when
the client omitted instructions, preventing an inferred developer guard from
changing the request contract.

The outbound identity source order remains valid credential-owner
`credentials.user_agent`, valid global `openai_codex_user_agent`, then the
compiled default. Fingerprint changes cannot select a different client family,
Originator, or session-policy result.

## Verification strategy

Run migration and generated-code checks, backend service/handler/repository
tests, frontend lint/typecheck/Vitest, release-document checks, and diff/import
policy checks. The PR remains reviewable and unpublished; release promotion is a
separate maintainer action.
