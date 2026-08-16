# Repository Instructions

These rules apply to the whole repository. Keep this file normative and short;
put commands, examples, and explanations in the linked documents.

## Sources of Truth

- App version: `backend/cmd/server/VERSION`
- Go toolchain: `backend/go.mod`
- Node.js/pnpm toolchain: `frontend/package.json`
- Release/lint tools: `.tool-versions`
- Development and checks: `CONTRIBUTING.md`
- Releases: `docs/RELEASING.md`
- Upstream mapping/status: `UPSTREAM.md`
- Database migrations: `backend/migrations/README.md`
- Deployment and security: `deploy/`

Do not duplicate current tool or release versions here.

## Change Rules

- Use pnpm only; update `frontend/pnpm-lock.yaml` with dependency changes.
- Do not edit generated Ent/Wire files. After schema changes, regenerate both
  and commit the output.
- When a Go interface changes, update all implementations, stubs, and mocks.
- Existing SQL migrations are immutable and forward-only. New files use a
  unique increasing prefix; `_notx.sql` is only for concurrent indexes.
- New configuration fields need defaults or environment bindings, tests, and
  synchronized examples under `deploy/`.
- Update provider/protocol docs for endpoint, auth, billing, quota, scheduling,
  default, or error-behavior changes.
- Keep README core section IDs and links aligned across all three languages.
  Put details under `docs/` or `deploy/`, not in README files.
- Keep frontend English and Chinese locale keys aligned.
- Codex outbound identity source precedence is immutable: a valid
  credential-owning account `credentials.user_agent` > a valid global
  `openai_codex_user_agent` > the compiled default. Empty or invalid candidates
  fall through only to the next source, and the compiled default is the final
  fallback. Version synchronization may update only the version declarations
  of the selected identity; it must not change the selected source, client
  family, Originator, OS, architecture, or terminal fingerprint. Inbound
  headers, generic header overrides, request classification, retries, probes,
  and upstream merges must not bypass this precedence. Keep User-Agent,
  Originator, and Version coherent, and update the source-priority matrix,
  exact default identity, and all outbound-path tests with every identity
  change.
- Use OpenSpec for cross-cutting public API, persistent-data, security-boundary,
  or multi-module changes; small fixes and docs-only changes need none.
- Never commit credentials, tokens, production configuration, or user data.
- Document only commands that exist in repository scripts or Make targets.

## Implementation Principles

- When current requirements replace behavior, remove the obsolete code. Do not
  retain backward compatibility, shims, legacy fallbacks, or migrations solely
  to preserve superseded behavior. This does not override requirements stated
  elsewhere in this file.
- Choose the simplest design that satisfies current requirements. Do not add
  speculative abstractions, configuration layers, or extensibility.
- First deliver the smallest runnable end-to-end implementation. Add layers or
  complexity only when a working flow demonstrates the need; do not replace
  working code to anticipate unfinished complexity.
- Keep components modular, with clear ownership and separation of concerns.
- Prefer maintained, established libraries over custom implementations unless
  there is a concrete reason not to.
- Inspect existing dependencies and project patterns before adding packages or
  writing custom infrastructure.
- Choose designs that meet known long-term requirements. Do not adopt a
  temporary solution that is knowingly intended to be replaced later.
- For architectural, public-interface, security-boundary, and other high-impact
  decisions, prefer proven patterns from mature products and maintained
  libraries over novel designs.

## Verification

Run focused checks from `CONTRIBUTING.md` while iterating. Ordinary branch
pushes use the fast `skills/push-cli push` path and must never target the
repository default branch. The final PR submission must use
`skills/push-cli submit-pr`; only that action runs the official local matrix
inside Apple Containers on macOS, Docker inside WSL2 Debian or Ubuntu on
Windows, and Docker on Linux. Host-side execution of that matrix is forbidden.
Release promotion must use `skills/release-cli`, the exact submit-pr base/head
proof, protected GitHub auto-merge without admin bypass, and successful PR and
merged-main Actions. Release metadata validation must not repeat the complete
local application matrix.
Backend changes need relevant Go tests; frontend changes need lint, typecheck,
and relevant Vitest coverage; locale, deployment, migration, and release
changes need their dedicated checks.

## Releases

- Per-release changes belong in GitHub Release notes, not README files.
- Tag, embedded version, Docker build args, and `UPSTREAM.md` must agree.
- Release notes must cover compatibility, known issues, and upstream baseline.
- Never reuse or retag a published version.
- Never push or commit release changes directly to `main`; preparation and
  post-publication status changes must arrive through separate PRs.
- Tag only the actual PR merge commit after its exact `main` push Actions pass.
- Keep tag publication, Release monitoring, verification, and finalization as
  separate resumable actions.
- Do not create, move, delete, or push tags, Releases, or images without an
  explicit publication request.
- Preserve intentional Plus changes during upstream merges and update
  `UPSTREAM.md` in the same change.
