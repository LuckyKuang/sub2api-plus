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
pnpm --dir frontend install --frozen-lockfile --config.confirmModulesPurge=false
```

## Documentation and Skill Maintenance

This contract applies to documentation, repository rules and skills used or
affected by the current task, including their supporting references, scripts
and tests. Check the relevant scope as you work; it does not require a complete
repository audit on every task.

For descriptive documentation and implementation conventions only:

> When a repository’s documentation and its existing code disagree, follow the conventions used in the existing code, and in your final reply point out each inconsistency you found between the documentation and the code.

Explicit current requirements, security boundaries and authoritative protocol
and test contracts take precedence over existing implementation. Existing code
is evidence of an implementation convention, not proof of business correctness.
When code violates a governing requirement, fix the code and its regressions;
do not rewrite requirements or weaken tests to legitimize the defect.

During implementation or skill use:

1. Compare the affected instructions and examples with the relevant code,
   maintained entrypoints and governing requirements. Follow the established
   code convention when descriptive guidance is stale, and verify commands in
   the prescribed environment before documenting or using them.
2. Correct discovered errors and synchronize every source affected by a behavior
   change in the same change. Include owning documentation, repository rules,
   skills, their references, executable helpers and applicable tests. Remove
   superseded instructions instead of leaving conflicting guidance in place.
3. Maintain each source in its owning location. Update authorized writable
   external skills there; do not create a competing repository copy. If an
   external or read-only source cannot be updated, record its location, the
   remaining change and the reason in the final reply.
4. Treat "current" as the verified implementation, approved requirements and
   applicable authoritative sources. Keep version references tied to the
   repository's maintained declarations rather than duplicating version pins.
5. In the final reply, list each discovered documentation/code inconsistency
   with both locations, the difference, its resolution and any outstanding
   update with its reason. Apply the same reporting to rule and skill issues.
   If none were found, state that for the inspected scope; do not imply an
   unperformed repository-wide audit.

Changes to the AGENTS.md maintenance contract also update the `compress-cli`
skill, its reference, validator and requirement-based regression tests. Run
their focused checks in the platform validation container as described below.

## Requirement-based Test Design

This is a mandatory repository-wide contract for unit, integration, UI, and
tooling tests. Derive test scenarios and expected results independently from
actual business requirements, acceptance criteria, and authoritative contracts.
Reading implementation code to find entrypoints, dependencies, and test setup
is allowed; treating its current output or branches as the definition of correct
behavior is not.

For each changed behavior:

1. Identify the requirement and its source before choosing assertions. Record
   the relevant requirement, documented rule, or official protocol/source
   reference in the test or fixture so reviewers can check the expectation
   independently. Resolve unclear requirements instead of silently declaring
   current behavior correct.
2. Use independent expected values or invariants. Do not call the code under
   test, reuse its generated output, or copy its algorithm to calculate the
   expected result. Protocol fixtures must have an authoritative source;
   snapshots require review against that source or the business requirement.
3. Cover applicable success, failure, boundary, and required/forbidden side
   effects. Verify observable behavior at the boundary where the requirement
   matters: for example, capture actual outbound requests for header privacy,
   or check that a rejected payment leaves balances and orders unchanged.
   Helper return values or mock call counts alone cannot prove those outcomes.
   Mocks may isolate external dependencies but must not replace the behavior
   the test claims to verify.
4. When implementation conflicts with the requirement, fix the implementation.
   Never weaken assertions, remove cases, or update expected values/snapshots
   merely to make tests pass. A legitimate requirement change must update its
   owning documentation and tests together.
5. Defect regressions must detect the original incorrect behavior and pass with
   the fix. Where feasible, demonstrate this against the pre-fix implementation
   or by restoring the specific defect in isolation; otherwise document the
   limitation and the evidence used. A failure caused only by a missing symbol
   or broken setup does not demonstrate detection of the business defect.

Passing tests and coverage percentages alone do not establish business
correctness. Review the requirement-to-assertion mapping and any untested
outcomes. The AGENTS.md validator protects this rule against removal or
weakening; it cannot determine whether a test's expected behavior is correct.
Provider identity tests additionally follow the
[requirement-based outbound regressions](docs/OUTBOUND_IDENTITY.md#requirement-based-regression-coverage).

## Development Checks

Selection dropdowns use `frontend/src/components/common/Select.vue` and the shared
panel in `SearchableSelect.vue` for both closed and expanded appearance. Do not
introduce native HTML `select` controls or use `.input` for dropdowns: native
expanded menus cannot reliably share the system's theme and option styling.
Use typed option values, disabled options, and `kind: 'group'` headers. The default
`searchable: 'auto'` preserves local search above five options; explicit
`searchable: false` hides only the search input. Clearable and custom
`selected`/`option` slot controls preserve integrated selected content and rich options.
Keep existing search-result popovers and their styling for user, account, model,
and proxy selectors. Search remains inside the dropdown; do not expose a
separate search input above or alongside a second selection field.
Checkbox/multi-selection and other composite controls retain their existing
dropdown layouts and interactions. Keep checkboxes inside the dropdown and
allow repeated selection without reopening it. Keep combined auto-refresh
enable/interval actions in their original menu. Live requests are the explicit
exception: show a temporary auto-refresh switch directly, with the interval
choices retained in the integrated refresh menu. User usage history uses its
existing manual Refresh and Reset buttons without a second refresh row or
background polling. Do not replace composite
controls with plain selectors or split them into separate visible fields.
The shared dropdown owns keyboard navigation and focus, including disabled/group
skipping, selection, Escape, and Tab. Keep trigger styles in `frontend/src/style.css`
and expanded-menu styles in `SearchableSelect.vue`, including light/dark surfaces,
borders, typography, hover, focus, selected, error and disabled states. Keep spacing
shared, and do not add local appearance replacements. Use `triggerClass` only for
compact height, text sizing or font; layout classes belong on the wrapper.
Individual pages may adjust layout and compact width, height or text size, leaving
enough space for the longest supported translation and the dropdown arrow.
Verify the expanded panel itself in both themes and narrow viewports, including
menus inside dialogs and scrollable cards; checking only the closed trigger is insufficient.

Data-table query rows read one control width everywhere. `.filter-bar` is the row
and `.filter-field` wraps exactly one control; both live in
`frontend/src/styles/workspace.css` and take their sizes from
`--filter-control-width` (160px, shared by search boxes and selects; the
search token is an alias, not a wider size), `--filter-date-width` (200px,
`.filter-field--date`, for date and date-range pickers only) and
`--filter-gap`. Action buttons stay outside `.filter-field` so they keep their
natural width, and labels belong inside the field they name. Controls fill their
field (`width: 100%`), so a select wrapper can never size itself to its longest
option label. Below 1024px fields stack at full width instead of squeezing.
Expanded select menus stay content-sized (`w-max`), floored at 200px (never
narrower than `--filter-control-width`) and capped at 480px so one long label cannot stretch the
panel across the viewport. `src/styles/__tests__/filterWidths.spec.ts` owns the
stylesheet contract and the `SearchableSelect` floor; the component layout suite
measures the rendered rows. The 480px ceiling also constrains the menu minimum
when its trigger is wider; a smaller visible viewport relaxes the normal floor.
Do not add ad-hoc per-page control widths to a query
row. Dense monitor toolbars that cannot fit the unified width without breaking
their workspace layout (currently the live requests bar and the channel status
matrix toolbar) keep their own compact widths.

Keep fields and `.filter-actions` in one wrapping `.filter-bar`; use
`.filter-bar--inline` on intermediate component wrappers so they do not reserve
a separate lane. Actions follow the final field, align to the control bottoms,
and use their natural widths with the group aligned right. A label displayed
beside its control stays with it in `.filter-pair`. Embedded `WorkspaceTabs`
use `.workspace-tabs--embedded` so the owning card supplies the only frame and
padding.

Console shapes and control sizes come from one set of tokens in
`frontend/src/styles/console.css`. Every rounded element takes exactly one step
of the radius ladder by role: container `--surface-radius` (16px: shell chrome,
page zones, dialogs, toasts) > card/panel (12px: business cards, floating menus,
popovers) > control/item (8px: buttons, fields, select and date triggers, tabs,
menu items, table rows) > tag (6px: badges, chips, segmented options, compact
buttons) > xs (4px: checkboxes, inline code). No breakpoint enlarges a step.
Tailwind `rounded-*` utilities resolve to these tokens (`sm`/default xs, `md`
tag, `lg` control, `xl` card, `2xl` container) and pages use `rounded-md`,
`rounded-lg` or `rounded-xl`; the container radius belongs to the shared
container classes. Square icon tiles up to 40px take the control radius.
`rounded-full` is reserved for avatars, status dots, switches, progress tracks
and spinners and never wraps text. Stylesheets declare radii only through the
ladder tokens. Buttons, fields, select and date triggers and tabs share
`--control-height` (36px), with `-sm` 32px, `-xs` 26px and `-lg` 44px variants;
button labels break only between words, never inside one. Status labels
use `.badge` (20px tag). Dialogs, menus and popovers use `--surface-panel`,
which stays near-opaque, and the dialog scrim dims by a fixed amount; only
in-page surfaces follow the card transparency setting.
`src/styles/__tests__/radiusLadder.spec.ts` and `controls.spec.ts` own this
contract.

Every record table uses `.record-table` from `frontend/src/styles/tables.css`:
every record is its own card (soft fill, hairline outline, item-radius corners,
a small gap to its neighbours); hover and selection deepen the fill and tint
the outline, and a frozen actions column carries a leading hairline. In
hierarchical tables every row is still a separate card: mark summary rows
`.record-row-parent` (stronger fill) and their expanded rows `.record-row-leaf`
(lighter fill, indented). Keep `.record-group` for one business record that
spans several rows through `rowspan`.
Row actions show at most two everyday actions as `.table-action` buttons
followed by one "more" menu; use `components/common/ActionMenu.vue` for a new
menu, with `mutates` set on actions that read-only assistance must lock. Every
list pages through `components/common/Pagination.vue` with numbered pages; do
not hand-build previous/next pagers. Use `embedded` inside a card or dialog,
`disabled` while loading, and `:total="null"` with `has-more` for cursor lists.
Two-to-four way view switches use `.segmented-control`/`.segmented-option` with
`aria-pressed`.

All validation, including focused checks while iterating, must run in the
platform validation container: Colima Docker containers on macOS, Docker inside WSL2
Debian or Ubuntu on Windows, and Docker on Linux. Do not run tests, lint,
typechecking, builds, policy checks, or other validation on the host.
After every validation attempt, successful or failed, remove the one-shot
project validation container, temporary resources, and historical writable
snapshots. Retain the Sub2API validation image whose deterministic identity
matches the current resolved Go, Node, pnpm, golangci-lint, GoReleaser,
govulncheck, Compose and PyYAML pins.
Retain dependency caches only for the generation matching that image and the
current Go and pnpm lock inputs. Remove stale Sub2API validation generations;
never prune unrelated projects or global runtime, builder, image, volume, or
system resources. Cache removal restores owner write permission on directories
inside the discarded generation so Go's read-only module cache can be removed;
it never follows cache symlinks or changes permissions in retained generations.
On Windows the launcher initializes the WSL cache root and enumerates direct
child paths without newline-valued command arguments, so cleanup also works
on a fresh installation and across the Windows/WSL command boundary.
The launcher cleans up stale validation images and caches before each run.
On every platform it also sweeps stopped project containers by the
`sub2api-validation` label, including those left by a hard kill that `--rm`
could not reclaim. Colima builds and cleanup use the verified profile's Docker
context and its matching Buildx builder. Named Colima profiles have separate
host dependency-cache roots so cleanup cannot remove another profile's active
mounts; see [scoped cleanup](docs/VALIDATION_OPERATIONS.md#scoped-cleanup).
To reclaim resources in the selected runtime/profile,
including validation images, caches and labelled containers, run
`python3 skills/push-cli/scripts/push_cli.py clean --yes`; it is manual only,
and it deletes the current generation, so the next validation rebuilds the
toolchain and redownloads dependencies.

WSL full checks use a fresh exact-commit checkout on Linux storage and retain
only deterministic dependency caches. The launcher owns the WSL session until
exit. Automatic cleanup defers when another validation is running; otherwise it
also removes precisely owned validation BuildKit records. See
[validation operations](docs/VALIDATION_OPERATIONS.md) for the direct-bind
comparison option, I/O benchmark, proxy setup and persistent timing summaries.

Inside that container, with GNU Make available, run the repository checks from
the root:

```bash
make test
```

The available check commands are listed below. The `./...` Go commands and
frontend `test:run` cover their full suites; select the affected packages or
components while iterating, and use `submit-pr` for the complete final matrix.
Integration tests always use `-count=1` so the current run actually sets up its
services and executes the business scenarios. Go's test-result cache does not
prove the current PostgreSQL/Redis state or track environment reads made before
`TestMain` calls `m.Run`. Compilation and dependency caches remain reusable.

The full and CI deployment gates also run `bash deploy/tests/install-runtime-resources-test.sh`.
It uses real archives and installation functions to cover fresh installs, upgrades,
missing pricing resources and copy failures. Release packaging tests exercise the
pinned GoReleaser and reject archives without the exact default pricing file and
captured-source bytes before upload. Run these checks in the validation container.

```bash
# Backend
cd backend
go mod tidy -diff
go test -tags=unit ./...
go test -count=1 -tags=integration ./...
golangci-lint run ./...
# Alternative for a memory-constrained validation container; same full scope
GOGC=35 GOMEMLIMIT=4GiB golangci-lint run --concurrency=1 --timeout=10m ./...

# Frontend, from the repository root
pnpm --dir frontend run lint:check
pnpm --dir frontend run typecheck
pnpm --dir frontend run test:run
pnpm --dir frontend run build

# Repository AGENTS.md contract
python3 skills/compress-cli/scripts/compress_cli.py check AGENTS.md
python3 skills/compress-cli/tests/test_compress_cli.py
node --test skills/sub2api-admin/tests/test_sub2api_admin.cjs
python3 tools/check_test_build_tags.py

# Seven-provider critical business regressions (run inside the same container)
python3 tools/check_seven_provider_coverage.py --profile unit
python3 tools/check_seven_provider_coverage.py --profile integration
python3 tools/check_seven_provider_frontend.py

# Focused owner/transport race checks; the pinned image includes CGO tools
CGO_ENABLED=1 go test -C backend -race -tags=unit ./internal/service -run 'Test(CNAPIKey|KimiAPIKey|OpenAIQuota.*Owner|LiveSidebandProduction)' -count=1
CGO_ENABLED=1 go test -C backend -race -tags=integration ./internal/repository -run 'Test(CNAPIKey|CNControlSnapshot|CNBalanceSchedule|CNProxyOwner|OpenAIQuotaOwner)' -count=1
```

Run the focused tests for the changed package or component inside the same
platform validation container while iterating.

For Vue component suites affected by guest clock corrections, freeze `Date`
before mounting with `vi.useFakeTimers({ toFake: ['Date'] })` and restore it
after each test. A backward correction can otherwise make Vue reject
an event as older than its listener, even when the prior action was awaited.
Keep scheduling timers real unless the scenario requires timer control; the
account-creation suite includes a regression against a backward system clock.

The [seven-provider matrix](docs/SEVEN_PROVIDER_TEST_MATRIX.md) maps the critical
provider scenarios to their observable assertions. The full validation profile
also runs its unit, integration and frontend gates. These execute the required tests and
reject missing, skipped, failed or unfinished cases, including all fourteen
seven-provider failover scenarios and forty-two Chat proxy-binding scenarios;
they do not replace the complete suites.
The integration gate uses the same disposable PostgreSQL/Redis environment as
the repository integration suite. Keep the matrix and gate manifest synchronized
when adding sending entrypoints or changing the owning business contracts.

The separate S3 storage gate requires disposable PostgreSQL, Redis and MinIO
reachable from the validation container. Set `SUB2API_TEST_POSTGRES_DSN`,
`SUB2API_TEST_REDIS_ADDR`, an explicit nonzero `SUB2API_TEST_REDIS_DB`, and
`SUB2API_ASYNC_TEST_S3_ENDPOINT` for those temporary services. MinIO must use the
fixture credentials `minio` / `minio-test-secret`. Inside the validation container,
use the same database for the migration smoke and the storage workflow:

```bash
export SUB2API_ASYNC_TEST_DATABASE_URL="$SUB2API_TEST_POSTGRES_DSN"
python3 tools/check_seven_provider_coverage.py --profile storage
```

This gate runs the existing migration/idempotency smoke before the real
asynchronous-image MinIO workflow, with the integration build tag and uncached
execution. Missing configuration, missing tests and skipped/failed tests reject
the gate; migration failure stops the workflow. Ordinary `unit`/`integration`
coverage profiles and the shared full/CI profile do not provision MinIO or run
this separate storage gate. Set the two `SUB2API_ASYNC_TEST_*` variables inside
the validation container; the launcher does not forward them. Remove all
temporary service containers and their writable state after validation.

The separate [real Chromium management suite](frontend/e2e/README.md) runs
`python3 tools/run_seven_provider_browser.py` as root inside a disposable
validation container with explicit disposable PostgreSQL/Redis settings and
`SUB2API_E2E_ISOLATED=1`. Its runner requires all fourteen browser scenarios to
execute successfully; missing/skipped cases fail. The ordinary frontend gate
checks Vue/API lifecycle regressions and does not claim browser execution.
For shared styling, dialog and dropdown changes, run
`pnpm --dir frontend run test:layout` in the validation container as well. This
independent [component layout suite](frontend/layout-tests/README.md) uses real
Chromium geometry and hit testing without backend accounts or services; it does
not replace the management business suite.
Authorization regressions must use redacted account DTOs and the initiating
`oauth_credential_owner`, include both owner-replacement isolation and unchanged
owner success controls, and retain pending grants across unrelated observation
updates. A late HTTP response test proves UI isolation; an already-issued OAuth
apply needs separate server owner/CAS and persistence assertions. Keep these
layers explicit in the [supported business inventory](docs/SEVEN_PROVIDER_SUPPORTED_BUSINESS.md)
and matrix rather than inferring provider authorization or billing from browser
proxy-management cases.

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

CI runs full frontend tests/build, native amd64/arm64 unit suites and
Docker-backed integration in pinned validation containers. See the shared
coverage inventory and pending authority cutover in
[`docs/CI_VALIDATION.md`](docs/CI_VALIDATION.md). The required local submission
gate remains active until that cutover is independently verified.

Intermediate branch pushes use the fast path and do not run local tests.
Remote `CI` and `Security Scan` run on pull requests and on `main` pushes, not
on every feature-branch push. Verified post-publication finalization retains
focused PR checks and uses `[skip ci]` only on its protected main merge; see
[the finalization contract](docs/RELEASING.md#finalize-through-a-pr).
`watch` follows the pull-request runs when a PR
exists, otherwise the `main` push runs:

```bash
python3 skills/push-cli/scripts/push_cli.py push
```

Before creating or updating the final pull request, run the promotion gate:

```bash
python3 skills/push-cli/scripts/push_cli.py submit-pr
```

`submit-pr` defaults to the `full` profile. It requires the latest
default-branch base and runs the complete matrix inside Colima Docker containers on
macOS, Docker inside WSL2 Debian or Ubuntu on Windows, and Docker on Linux.
Backend tests run before backend lint/policy while the frontend lane runs
concurrently within the 4-CPU/8-GiB container and report step/lane wall-clock durations; no check is
removed. Go test/lint, including the seven-provider Go gates, and frontend
production builds share one memory slot to avoid cold-cache memory exhaustion;
other frontend checks may overlap Go work.
The launcher fixes `GOMEMLIMIT=6GiB` for Go processes inside the 8-GiB VM,
including cold-cache lint. This soft managed-memory target leaves runtime
headroom; it is not a hard RSS limit or a replacement for the shared memory slot.
Host-side execution of any validation is forbidden. For diagnosis or a
same-commit timing baseline, pass `--serial` to `check`.
Production builds force fresh TypeScript compiler state with `vue-tsc -b --force`;
dependency caches remain reusable. This ensures incremental state cannot hide a
production compiler memory failure. Every step streams progress and records a
content-free execution summary outside the disposable dependency cache.

After validation, `submit-pr` rechecks and pushes the exact candidate, writes and
reads back the PR's typed base/head marker, then publishes local success last so
immediate auto-merge cannot race the marker update.

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

For integration tests without a nested Docker API, the launcher forwards
`SUB2API_TEST_POSTGRES_DSN`, `SUB2API_TEST_REDIS_ADDR`,
`SUB2API_TEST_REDIS_DB`, `SUB2API_TEST_REDIS_USERNAME` and
`SUB2API_TEST_REDIS_PASSWORD` by environment name, keeping credentials out of
command logs. External Redis requires an explicitly selected nonzero database;
missing or invalid selection fails instead of falling back to DB 0. The route
rate-limit integration test also accepts this isolated external Redis.

On Windows the launcher exports the configured proxy/integration variable names
through its process-local `WSLENV` before invoking Docker inside WSL. It preserves
existing entries and does not put credential values in command arguments or
persist changes to Windows user settings.

Prompt Audit uses a separate disposable database through
`PROMPT_AUDIT_TEST_POSTGRES_DSN`, and separate Redis selection through
`PROMPT_AUDIT_TEST_REDIS_ADDR` and `PROMPT_AUDIT_TEST_REDIS_DB`; its Redis
credentials use the same `SUB2API_TEST_REDIS_USERNAME` / password bindings.
These variables are also forwarded by name. Never share its database with the
repository suite: both suites write and truncate fixtures independently.

Supply disposable databases reachable from the validation container.
`SUB2API_VALIDATION_DOCKER_NETWORK` selects an existing project-owned Docker
network on Colima, WSL2 or Linux; reserved `host`, `none` and `bridge` values are
rejected. Unset leaves Docker networking unchanged.
See [network configuration](docs/VALIDATION_OPERATIONS.md#macos-colima).
Reusing PostgreSQL requires dedicated test databases and
credentials. Redis logical databases do not isolate Pub/Sub: suites exercising
invalidation must use a dedicated temporary Redis process, which may run inside
the existing Redis container on a separate port with persistence disabled.
Never grant tests access to live subscription channels. Reserve initially empty
test Redis databases and remove only their test keys afterward. Delete disposable
PostgreSQL databases, test roles/ACL users, temporary Redis processes/files and
containers after validation, including failed attempts; preserve live services
and business data.

Temporary Redis must run in the foreground under a supervisor that waits for
its child. Do not daemonize it into an existing container whose PID 1 cannot
reap arbitrary orphaned children. Keep its process handle and verify process
exit and socket/port closure before deleting its configuration and credentials.
An accepted `SHUTDOWN NOSAVE` command alone does not prove cleanup succeeded.

Use `redis-cli -e` for every scripted health or cleanup command: the default CLI
can return zero on `WRONGPASS`, `NOAUTH` or `NOPERM`. For a test-owned process,
supply the selected credential through stdin and export it in the child shell.
The Colima example uses the explicitly verified context:

```bash
printf '%s\n' "$validation_redis_password" | \
  docker --context colima exec --interactive "$validation_redis_container" sh -c '
IFS= read -r REDISCLI_AUTH || exit 1
if [ -n "$REDISCLI_AUTH" ]; then
    export REDISCLI_AUTH
else
    unset REDISCLI_AUTH
fi
exec redis-cli -e -p "$1" SHUTDOWN NOSAVE
' sh "$validation_redis_port"
```

Redis readiness regressions use real `redis-server`/`redis-cli` from the
validation image, private Unix sockets, no TCP listeners and no persistence.
Run `python3 deploy/tests/test_redis_health.py` inside that container; the full
local matrix and CI `deployment-config` lane include the same suite.

The `release-finalization` profile is not a general fast option. Only
`release-cli finalize` may request it for a verified published tag and a tree
that can be regenerated exactly from its recorded base. Both profiles bind the
exact base/head SHAs, and finalization also binds the tag. Release PR merging
and publication use `skills/release-cli` after GitHub required checks pass.
Focused release metadata and deterministic finalization checks use the same
platform validation container; they never fall back to the host or repeat the
application matrix. Git/GitHub operations and runtime management remain with
the host launcher.

Finalization automatically promotes its exact deterministic PR through protected
auto-merge after its focused PR gates pass. Its deterministic merge subject ends
with `[skip ci]`; completion verifies the actual merge parents/tree and main
containment rather than waiting for skipped main workflows. Branch commits must
still run PR workflows. Release candidates continue to require CI and Security
Scan at the exact merged-main SHA. Pending PR checks/merge return status 2 and a
retry command; already merged finalization resumes only after independent
proof/tree/publication and merge checks.
See [the release process](docs/RELEASING.md) and
[distributed packaging and read-only rehearsals](.github/release-tools/README.md).

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
