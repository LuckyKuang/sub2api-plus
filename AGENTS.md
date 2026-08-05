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
- Codex built-in outbound identity is an invariant: with no account UA, no
  global UA, or an invalid legacy global UA, every built-in fallback must
  resolve the same complete User-Agent/Originator/Version triple. Keep those
  fields derived from one source of truth and update exact identity and
  fallback-path tests together; account custom UA > global UA priority remains
  unchanged.
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

Run checks appropriate to the changed paths as listed in `CONTRIBUTING.md`.
Backend changes need relevant Go tests; frontend changes need lint, typecheck,
and relevant Vitest coverage; locale, deployment, migration, and release
changes need their dedicated checks.

## Releases

- Per-release changes belong in GitHub Release notes, not README files.
- Tag, embedded version, Docker build args, and `UPSTREAM.md` must agree.
- Release notes must cover compatibility, known issues, and upstream baseline.
- Never reuse or retag a published version.
- Do not create, move, delete, or push tags, Releases, or images without an
  explicit publication request.
- Preserve intentional Plus changes during upstream merges and update
  `UPSTREAM.md` in the same change.
