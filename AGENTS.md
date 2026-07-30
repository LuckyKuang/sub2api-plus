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
- Use OpenSpec for cross-cutting public API, persistent-data, security-boundary,
  or multi-module changes; small fixes and docs-only changes need none.
- Never commit credentials, tokens, production configuration, or user data.
- Document only commands that exist in repository scripts or Make targets.

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
