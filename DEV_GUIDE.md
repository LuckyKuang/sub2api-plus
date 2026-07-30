# Sub2API Plus Development Guide

This guide contains local setup and troubleshooting notes. Mandatory repository
rules are in [`AGENTS.md`](AGENTS.md), and contribution checks are in
[`CONTRIBUTING.md`](CONTRIBUTING.md).

## Toolchain

Use repository-declared versions:

- Go: `backend/go.mod`
- Node.js and pnpm: `frontend/package.json`
- CI reference: `.github/workflows/backend-ci.yml`

Do not copy tool versions into additional policy documents.

## Local Services

Development requires PostgreSQL and Redis. Use local services or the development
Compose file:

```bash
docker compose -f deploy/docker-compose.dev.yml up -d
```

Keep local credentials in ignored `.env` or config files. Do not add developer
machine paths, passwords, or production data to tracked documentation.

## Install and Build

```bash
pnpm --dir frontend install --frozen-lockfile
pnpm --dir frontend run build

cd backend
go build ./cmd/server
```

The root Makefile also provides:

```bash
make build
make test
```

On Windows without GNU Make, run the underlying Go and pnpm commands directly.

## Run in Development

Backend:

```bash
cd backend
go run ./cmd/server
```

Frontend:

```bash
pnpm --dir frontend run dev
```

Copy configuration examples to ignored local files before editing them. See
[`deploy/README.md`](deploy/README.md) for service configuration.

## Verification

Backend:

```bash
cd backend
go test -tags=unit ./...
go test -tags=integration ./...
golangci-lint run ./...
```

Frontend:

```bash
pnpm --dir frontend run lint:check
pnpm --dir frontend run typecheck
pnpm --dir frontend run test:run
```

Deployment scripts are checked by `.github/workflows/backend-ci.yml`.

## Code Generation

After editing `backend/ent/schema`:

```bash
cd backend
go generate ./ent
go generate ./cmd/server
```

Commit generated Ent and Wire output. Do not edit it directly.

## Common Problems

### Frozen lockfile failure

If `frontend/package.json` changes, run `pnpm install` in `frontend` and commit
the resulting `pnpm-lock.yaml`.

### npm/pnpm installation conflict

Remove only the repository's `frontend/node_modules` after confirming the path,
then reinstall with pnpm. Do not use npm or yarn for this project.

### Interface compilation failure

When a Go interface gains a method, update all production implementations and
test stubs/mocks before running the broader test suite.

### Configuration ignored from the environment

New config fields need a registered default or explicit binding so Viper can
reach them through environment variables. Update the config tests and deployment
examples together.

### Database migration failure

Do not edit an applied migration to repair it. Restore the released content and
create a new compensating migration. See
[`backend/migrations/README.md`](backend/migrations/README.md).

### Forwarded client IP or proxy behavior

Review [`deploy/EDGE_SECURITY.md`](deploy/EDGE_SECURITY.md). Do not trust broad
proxy CIDRs or raw forwarded headers without an enforced edge boundary.

## Repository Layout

```text
sub2api-plus/
├── backend/        Go service, Ent schemas, migrations, and tests
├── frontend/       Vue application and en/zh locales
├── deploy/         Installers, Compose files, and operations documentation
├── docs/           Provider, protocol, and maintainer documentation
├── openspec/       Specifications for cross-cutting changes
└── tools/          Repository validation scripts
```

## Related Documents

- [Contributing](CONTRIBUTING.md)
- [Documentation index](docs/README.md)
- [Release process](docs/RELEASING.md)
- [Upstream mapping](UPSTREAM.md)
