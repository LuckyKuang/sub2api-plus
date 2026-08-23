# AGENTS.md
|Scope:Repository-wide|Keep this file normative and short|Put commands, examples, and explanations in linked sources of truth.
|Sources:App version=backend/cmd/server/VERSION|Go=backend/go.mod|Node/pnpm=frontend/package.json|release/lint=.tool-versions|development/checks=CONTRIBUTING.md|releases=docs/RELEASING.md|upstream=UPSTREAM.md|migrations=backend/migrations/README.md|deployment/security=deploy/|Do not duplicate current versions here.
|Dependencies:Use pnpm only|Update frontend/pnpm-lock.yaml for dependency changes|Inspect existing dependencies and project patterns before adding packages or infrastructure.
|Generated Code:Do not edit generated Ent/Wire files|After schema changes regenerate both and commit output.
|Interfaces:When a Go interface changes, update every implementation, stub, and mock.
|Migrations:Existing SQL migrations are immutable and forward-only|Use a unique increasing prefix|Use _notx.sql only for concurrent indexes.
|Configuration:New fields require defaults or environment bindings, tests, and synchronized deploy/ examples.
|Protocol Documentation:Update provider/protocol docs for endpoint, auth, billing, quota, scheduling, default, or error-behavior changes.
|README:Keep core section IDs and links aligned across README.md, README_CN.md, and README_JA.md|Put details under docs/ or deploy/.
|Locales:Keep frontend English and Chinese locale keys aligned.
|Codex Identity:Source precedence is immutable: valid credential-owning account credentials.user_agent > valid global openai_codex_user_agent > compiled default|Empty/invalid candidates fall through only to the next source|Version sync may update only selected identity version declarations and must not change source, client family, Originator, OS, architecture, or terminal fingerprint|Inbound headers, generic overrides, request classification, retries, probes, and upstream merges must not bypass precedence|Keep User-Agent, Originator, and Version coherent|Every identity change updates the source-priority matrix, exact default identity, and all outbound-path tests.
|Security Audit:Ingress and content extraction are immutable security boundaries|Every content-bearing HTTP/WS request or turn must be classified and audited after auth/basic validation but before account selection, billing, concurrency acquisition, upstream writes, or other side effects|API-key/OAuth account type, session affinity, routing, retries, probes, protocol adapters, transforms, request classification, and upstream merges must not bypass or weaken this boundary|Content Moderation and Prompt Audit must consume the same canonical protocol extraction contract|Only explicitly classified control-only frames may be no-content|A content-bearing extraction failure, including any partial/incomplete extraction hidden by successful sibling content, must be observable and fail closed whenever a blocking audit mode is active|Every endpoint, payload, or tool-field change must update docs/SECURITY_AUDIT_CONTENT_COVERAGE.md, real-payload semantic tests for both engines, and HTTP/WS/account-type side-effect-order tests.
|OpenSpec:Required for cross-cutting public API, persistent-data, security-boundary, or multi-module changes|Small fixes and docs-only changes need none.
|Secrets:Never commit credentials, tokens, production configuration, or user data.
|Documented Commands:Only document commands that exist in repository scripts or Make targets.
|Implementation:When requirements replace behavior, remove obsolete code|Do not retain superseded compatibility, shims, fallbacks, or migrations unless another rule requires them.
|Design:Choose the simplest design that satisfies current requirements|Deliver the smallest runnable end-to-end flow first|Add abstraction only for demonstrated need|Keep modular ownership and separation of concerns|Prefer maintained libraries and proven high-impact patterns|Do not choose knowingly temporary architecture.
|Verification:Run focused checks from CONTRIBUTING.md while iterating|Backend changes require relevant Go tests|Frontend changes require lint, typecheck, and relevant Vitest|Locale, deployment, migration, and release changes require dedicated checks.
|Push:Ordinary branch pushes use skills/push-cli push|Never target the repository default branch.
|Submit PR:Final submission uses skills/push-cli submit-pr|Only submit-pr runs the official local matrix in Apple Containers on macOS, Docker in WSL2 Debian/Ubuntu on Windows, and Docker on Linux|Host-side execution of that matrix is forbidden.
|Release Promotion:Use skills/release-cli with exact submit-pr base/head proof, protected GitHub auto-merge without admin bypass, and successful PR plus merged-main Actions|Release metadata validation must not repeat the complete local application matrix.
|Release Notes:Per-release changes belong in GitHub Release notes, not README files|Cover compatibility, known issues, and upstream baseline.
|Release Consistency:Tag, embedded version, Docker build args, and UPSTREAM.md must agree|Never reuse or retag a published version.
|Release Flow:Never push or commit release changes directly to main|Preparation and post-publication status changes require separate PRs|Tag only the actual PR merge commit after its exact main push Actions pass|Keep tag publication, Release monitoring, verification, and finalization separate and resumable.
|Publication Safety:Do not create, move, delete, or push tags, Releases, or images without explicit publication request.
|Upstream Merge:Preserve intentional Plus changes and update UPSTREAM.md in the same change.
|Local Skill:skills/compress-cli
|Skill Trigger:Use compress-cli when a request creates, compresses, validates, or updates AGENTS.md repository rules.
