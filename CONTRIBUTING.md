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

Install frontend dependencies with pnpm only and synchronize
`frontend/pnpm-lock.yaml` when they change. Go dependency changes synchronize
`backend/go.mod` and `backend/go.sum`.

Inside the validation container:

```bash
pnpm --dir frontend install --frozen-lockfile
```

## Development Checks

Plain selection dropdowns use native HTML `select` controls and shared styles in
`frontend/src/style.css`. Reuse `frontend/src/components/common/Select.vue` for
typed values, disabled options, and option groups. Its default `searchable: 'auto'`
preserves local search when there are more than five options; explicit
`searchable: false` keeps a plain dropdown native. Searchable, clearable, and
custom `selected`/`option` slot controls use the existing popover in
`SearchableSelect.vue`, preserving integrated selected content and rich options.
Keep existing search-result popovers and their styling for user, account, model,
and proxy selectors. Search remains inside the dropdown; do not expose a
separate search input above or alongside a second selection field.
Checkbox/multi-selection and other composite controls retain their existing
dropdown layouts and interactions. Keep checkboxes inside the dropdown and
allow repeated selection without reopening it. Keep combined auto-refresh
enable/interval actions in their original menu. Do not replace composite
controls with plain native selects or split them into separate visible fields.
Native controls own keyboard navigation and focus for plain dropdowns.
Keep horizontal padding and arrow placement in the shared styles; individual
pages may adjust control width and height.

All validation, including focused checks while iterating, must run in the
platform validation container: Apple Containers on macOS, Docker inside WSL2
Debian or Ubuntu on Windows, and Docker on Linux. Do not run tests, lint,
typechecking, builds, policy checks, or other validation on the host.
After every validation attempt, successful or failed, remove the one-shot
project validation container, temporary resources, and historical writable
snapshots. Retain the Sub2API validation image whose deterministic identity
matches the current resolved Go, Node, pnpm, golangci-lint, and GoReleaser pins.
Retain dependency caches only for the generation matching that image and the
current Go and pnpm lock inputs. Remove stale Sub2API validation generations;
never prune unrelated projects or global runtime, builder, image, volume, or
system resources. Cache removal restores owner write permission on directories
inside the discarded generation so Go's read-only module cache can be removed;
it never follows cache symlinks or changes permissions in retained generations.
On Windows the launcher initializes the WSL cache root and enumerates direct
child paths without newline-valued command arguments, so cleanup also works
on a fresh installation and across the Windows/WSL command boundary.
The launcher cleans up automatically before each run and
sweeps stopped project containers by the `sub2api-validation` label (a hard
kill that `--rm` cannot reclaim is still removed); on macOS the cross-project
Apple Builder cache stays under `deploy/APPLE_CONTAINER.md` manual guidance and
is never touched. To reclaim everything at once, run
`python3 skills/push-cli/scripts/push_cli.py clean --yes`; it is manual only,
and it deletes the current generation, so the next validation rebuilds the
toolchain and redownloads dependencies.

Inside that container, with GNU Make available, run the repository checks from
the root:

```bash
make test
```

The available check commands are listed below. The `./...` Go commands and
frontend `test:run` cover their full suites; select the affected packages or
components while iterating, and use `submit-pr` for the complete final matrix.

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

# Repository AGENTS.md contract
python3 skills/compress-cli/scripts/compress_cli.py check AGENTS.md
python3 skills/compress-cli/tests/test_compress_cli.py
python3 tools/check_test_build_tags.py
```

Run the focused tests for the changed package or component inside the same
platform validation container while iterating.

Provider/account changes, outbound paths, client versions, dependency upgrades
and upstream merges must preserve the trusted identity triple. Follow the
[mandatory outbound identity maintenance contract](docs/OUTBOUND_IDENTITY.md#mandatory-maintenance-contract)
and provide its applicable regression evidence before merging. Backend identity
changes also require the complete existing Codex identity regressions. Do not
weaken identity checks to accept upstream behavior. The repository AGENTS.md
validator protects these rules as well as the existing Codex and audit rules.

Backend `unit` tests stay in-process (mocks, memory SQLite, miniredis,
httptest). `integration` tests require Docker Postgres/Redis or an explicit
external DSN. Every `backend/**/*_test.go` file must declare one of those
build tags (or `e2e` / `embed` / a documented exception). In-process tests that
must remain visible to default `golangci-lint` use `unit || !integration`.
Helpers shared by unit, integration, and default `golangci-lint` use `!e2e`.

Intermediate branch pushes use the fast path and do not run local tests.
Remote `CI` and `Security Scan` run on pull requests and on `main` pushes, not
on every feature-branch push. `watch` follows the pull-request runs when a PR
exists, otherwise the `main` push runs:

```bash
python3 skills/push-cli/scripts/push_cli.py push
```

Before creating or updating the final pull request, run the promotion gate:

```bash
python3 skills/push-cli/scripts/push_cli.py submit-pr
```

`submit-pr` defaults to the `full` profile. It requires the latest
default-branch base and runs the complete matrix inside Apple Containers on
macOS, Docker inside WSL2 Debian or Ubuntu on Windows, and Docker on Linux.
Independent backend-test, backend-lint/policy, and frontend lanes run with
bounded concurrency and report step/lane wall-clock durations; no check is
removed. Host-side execution of any validation is forbidden. For diagnosis or a
same-commit timing baseline, pass `--serial` to `check`.

Linked worktrees use the same launcher. It also mounts their shared Git
metadata so base/head and policy checks can inspect the actual branch. On
Windows, use relative Git worktree metadata pointers with forward slashes so
both Git for Windows and Git inside WSL resolve them. Other working trees and
live Compose deployments are not mounted by this additional Git mount.

If a Windows loopback proxy works from WSL but cannot be reached reliably
through Docker's bridge, set `SUB2API_VALIDATION_WSL_NETWORK=host` for the
validation launcher process. This explicit WSL2-only option shares WSL's
network namespace, allowing its existing loopback proxy; all checks still run
inside the pinned Docker container. It does not start or reconfigure Compose
services. Leave it unset to use the default Docker network.

For repository integration tests without a nested Docker API, the launcher
forwards `SUB2API_TEST_POSTGRES_DSN`, `SUB2API_TEST_REDIS_ADDR`, and optional
`SUB2API_TEST_REDIS_PASSWORD` by environment name, keeping credentials out of
command logs. Supply disposable, isolated services reachable from the
validation container; the integration suite applies migrations and writes
fixtures. Remove these services and their data after the validation attempt.

The `release-finalization` profile is not a general fast option. Only
`release-cli finalize` may request it for a verified published tag and a tree
that can be regenerated exactly from its recorded base. Both profiles bind the
exact base/head SHAs, and finalization also binds the tag. Release PR merging
and publication use `skills/release-cli` after GitHub required checks pass.
Focused release metadata and deterministic finalization checks use the same
platform validation container; they never fall back to the host or repeat the
application matrix. Git/GitHub operations and runtime management remain with
the host launcher.

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
- Run `pnpm --dir frontend run check:i18n` in the validation container for locale
  schemas, static keys, dynamic API enum labels, message compilation, and matching
  interpolation parameters. The frontend build runs this check automatically.
- Keep the three README core section IDs aligned.
- Put detailed operational content in `docs/` or `deploy/`.
- Add user-visible changes to the release notes.
- Configuration changes need defaults, storage or environment bindings,
  relevant tests, and owning documentation appropriate to their source. Update
  `deploy/` examples when deployment configuration changes.
- Prefer maintained repository scripts and Make targets when documenting
  workflows. Verify native tool syntax, supported versions, and execution
  environments before documenting additional commands.

## Specifications

Use a local OpenSpec change to plan cross-cutting features or changes to public
APIs, persistent data, security boundaries, or multi-module behavior. The
`openspec/changes/` directory is intentionally untracked and must not be
included in pull requests. Start from the tracked example under
`openspec/examples/` when useful.

Record durable behavior in the owning documentation and automated tests. Use
pull request descriptions and commit history for change rationale. Small fixes
and documentation-only changes do not require an OpenSpec plan.

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

Never push `main` directly. The repository ruleset and local CLI must both
require pull requests. A PR head or default-branch base change after
`submit-pr` invalidates its local-validation proof and requires resubmission.
