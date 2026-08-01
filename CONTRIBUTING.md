# Contributing to Sub2API Plus

Thank you for contributing. Repository-wide mandatory rules are in
[`AGENTS.md`](AGENTS.md); this guide provides the working commands and review
flow.

## Toolchain

Use the versions declared by the repository:

- Go: `backend/go.mod`
- Node.js and pnpm: `frontend/package.json`
- Application release: `backend/cmd/server/VERSION`
- Release and lint tools: `.tool-versions`

Install frontend dependencies with pnpm only:

```bash
pnpm --dir frontend install --frozen-lockfile
```

## Development Checks

With GNU Make available, run the repository checks from the root:

```bash
make test
```

The equivalent focused commands are:

```bash
# Backend
cd backend
go mod tidy -diff
go test -tags=unit ./...
go test -tags=integration ./...
golangci-lint run ./...

# Frontend, from the repository root
pnpm --dir frontend run lint:check
pnpm --dir frontend run typecheck
pnpm --dir frontend run test:run
```

Run the focused tests for the changed package or component while iterating;
run the broader checks before opening a pull request.

## Generated Code

After changing `backend/ent/schema`, regenerate Ent and Wire:

```bash
cd backend
go generate ./ent
go generate ./cmd/server
```

Commit the generated output. Do not edit generated Ent or Wire files directly.

## Database Changes

Read [`backend/migrations/README.md`](backend/migrations/README.md) before
adding a migration. Existing migrations are immutable. Use the next unique
numeric prefix and create a forward-only migration.

## Documentation and Localization

- Update English and Chinese frontend locales together.
- Keep the three README core section IDs aligned.
- Put detailed operational content in `docs/` or `deploy/`.
- Add user-visible changes to the release notes.

## Specifications

Create or update an OpenSpec change for cross-cutting features or changes to
public APIs, persistent data, security boundaries, or multi-module behavior.
Small fixes and documentation-only changes do not require a new proposal.

## Pull Requests

Keep changes focused and use the existing commit style, such as `feat:`,
`fix:`, `test:`, `docs:`, and `chore:`. Complete the pull request checklist
and include:

- The problem and intended behavior
- Important implementation or compatibility decisions
- Tests performed
- Migration, configuration, documentation, and release-note impact

Release publication is a separate maintainer action. A pull request must not
create or move release tags.
