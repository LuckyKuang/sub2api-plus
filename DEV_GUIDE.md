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

Run these commands inside the platform validation container described in
[`CONTRIBUTING.md`](CONTRIBUTING.md#development-checks). On Windows, that means
a Docker validation container inside a WSL2 Debian or Ubuntu distribution.

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

Run these Make targets or their underlying Go and pnpm commands inside the
same validation container.

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

All validation, including focused tests, lint, typechecking, builds, and policy
checks, must run inside the platform validation container. On Windows, use
Docker inside WSL2 Debian or Ubuntu; running checks directly on Windows or
directly in the WSL2 distribution outside Docker is forbidden. On macOS, use
Apple Containers; on Linux, use Docker.

Follow [`CONTRIBUTING.md`](CONTRIBUTING.md#development-checks) for the pinned
toolchain, check selection, and validation-container cleanup requirements. Run
the relevant checks while iterating; final PR submission uses the required
complete matrix. The commands below run inside that container.

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

### WSL2 proxy is unavailable in Docker

When WSL2 uses a proxy, configure the standard `HTTP_PROXY`, `HTTPS_PROXY`, and
`NO_PROXY` variables with an address reachable from the Docker bridge before
invoking `push_cli.py`. A proxy bound to WSL2 loopback is not reachable from a
normal validation container; use an accessible Windows host-interface address.
The validation launcher forwards configured proxy variable names without
printing their values.

### Interface compilation failure

When a Go interface gains a method, update all production implementations and
test stubs/mocks before running the broader test suite.

### Configuration ignored from the environment

For configuration sourced from YAML or environment variables, register a
default or explicit binding so Viper can reach the field. Update relevant tests
and deployment examples. Database-backed settings follow their own defaults,
persistence, and documentation contract.

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
