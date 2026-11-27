# CI validation coverage and migration

This migration keeps the existing required local submission gate. Ordinary and
release-candidate PRs still use push-cli `submit-pr` with its full container
matrix. CI-only submission and a replacement trusted-success context are not
enabled by this coverage change.

## Maintained check definitions

[validation_checks.py](../tools/validation_checks.py) owns commands shared by
local full validation and CI. [ci_validation.py](../tools/ci_validation.py)
launches them through [validation_runtime.py](../tools/validation_runtime.py).
Host launchers inspect metadata, manage images/caches and launch containers;
application, policy and deterministic-tree checks execute inside containers.

The full integration command uses `go test -count=1 -tags=integration ./...` in
both local submission and CI. PostgreSQL/Redis availability and fixture state
must be exercised in the current run. Setup and environment reads before
`TestMain` calls `m.Run` are outside Go's test-input logging, and remote service
state cannot be established by a cached test result. This disables only test
result reuse; the pinned compilation and dependency caches remain available.

| Context / lane | Coverage | Relationship to local full |
| --- | --- | --- |
| `deployment-config` | Installer syntax and real archive/resource installation regressions, pinned in-container Compose parsing, real Redis authentication/readiness regressions, Docker security/gateway/resources, Caddy policy | Union of local and CI deployment checks; runtime regressions run in repository policy |
| `test` | Full unit suites on Linux amd64 and arm64; Docker-backed integration on amd64 | Same Go commands, additional architecture coverage |
| `frontend` | Frozen install, lint, typecheck, full Vitest, required seven-provider Vue/API cases, production build and its locale checks | Complete local frontend coverage; critical-only tests cannot substitute |
| `golangci-lint` | Complete backend lint | Same command |
| `repository-policy` | CLI/release/CI regressions, test-tag rules, identity, README, metadata and migrations | Shared full definition including local-only checks |
| `goreleaser-config` | Pinned configuration check with embedded Plus version | Additional CI check |
| `backend-security` | Pinned govulncheck | Additional security coverage |
| `frontend-security` | Production audit and documented exception gate | Same local audit helper; no duplicate audit in frontend CI |

For the full profile, the required `test` context requires both native unit jobs
and integration to succeed. Failed/skipped dependencies cannot pass through an
unconditional aggregator. Verified finalization instead requires successful
repository policy and deliberately skipped unit/integration jobs. Missing or
unknown profiles and failed policy always fail the aggregator.
The full local command inventory also runs CI regression tests and
gateway/test-tag checks formerly owned only by CI.
Release archive, workflow, OCI integrity and immutable pricing regressions and
the image script syntax check are also in both full profiles.
Admin CLI regressions run the real command against loopback HTTP fixtures in
both local full validation and the CI repository-policy lane. They cover the
JWT/password/TOTP export contract, body-only scope, output privacy and failures;
they never operate a deployed instance.
Validation operations/workspace regressions also run in repository policy and
the local full profile. They cover scoped cleanup, WSL lifecycle/encoding,
download failure, live progress, exact source isolation and PR proof ordering.

The image resolves Go, Node, pnpm, lint, GoReleaser, govulncheck, Compose and PyYAML from repository
declarations. Node uses an exact Node 24 LTS pin in
[`validation_runtime.py`](../tools/validation_runtime.py), mirrored by the
[`Dockerfile.validation`](../deploy/Dockerfile.validation) default, and must meet
the frontend's declared minimum engine version. The image identity includes
these pins and the Dockerfile. Dependency generation additionally includes Go
and pnpm lock inputs and the cache layout version. The pnpm store is explicitly
bound to the mounted Linux dependency generation rather than the source disk.
Frozen installation explicitly disables pnpm 9's modules-purge confirmation;
an offline regression checks actual dependency reconstruction and lockfile refusal,
rather than accepting a zero exit at an unanswered prompt.
Remote caches are
separated by architecture and lane; fallback preserves the exact generation.
GitHub ref visibility still applies: prefixes do not guarantee cross-tag reuse
or warm compilation. No validation image is pushed to a registry.
Go security patch updates synchronize `backend/go.mod`, all application
Dockerfiles and the validation Dockerfile default. The backend security lane
scans the pinned compiler's standard library as well as application modules;
standard-library findings require updating that compiler, not an audit exception.
The [Go release history](https://go.dev/doc/devel/release#go1.27.4) records the
`net/http` security fixes in the patch adopted for GO-2026-6859.
The image includes GCC and libc development headers so focused Go race tests
can execute with `CGO_ENABLED=1` under the same pinned container toolchain.
Compose's official Linux binary is pinned by `.tool-versions`, verified against
its release checksum and embedded version, and used only for `config --quiet`
against the four maintained Compose files and the tracked `.env.example`,
with explicit inert external-service values for the standalone template.
This shared local/CI gate needs neither a Docker socket nor deployment secrets;
there is no host-side Compose-validation exception.
Running an ordinary unit suite does not imply that its race profile was run.
The exact Playwright pin also determines the image's Chromium revision. Real
browser management tests use a separate root-owned disposable-service runner;
the full frontend lane's critical Vue/API tests do not imply browser execution.
Release frontend builds share the `frontend` cache lane with default-branch CI
under the same exact OS, architecture, toolchain and lock generation. They still
run frozen installation and the complete production build; other release lanes
retain their separate cache ownership.
Production builds force `vue-tsc -b --force` so restored incremental compiler
state cannot bypass the full compiler or conceal heap exhaustion. Lane execution
writes private JSON summaries with exact head/base/toolchain/cache identity,
initial dependency-cache presence, step timing and aggregate container counters.
These summaries neither establish remote cache hits nor replace submission proof.

## Containers and platform coverage

CI uses Docker on Linux and executes unit suites natively on amd64/arm64.
Local checks use Colima Docker containers on macOS, Docker in supported
WSL2 Debian/Ubuntu, or Docker on Linux, with existing scoped cleanup.
The shared launcher fixes `GOMEMLIMIT=6GiB` inside its 8-GiB validation container,
so cold-cache Go lint leaves headroom without changing the command, linter scope
or heavy-step scheduling. This is a per-process soft managed-memory target;
non-Go memory and aggregate process RSS remain outside that target. Host
environment values cannot disable or replace the declared container setting.
The same launcher sets `NODE_OPTIONS=--max-old-space-size=4096` for a 4-GiB
Node old-space budget. Node's host-dependent default can be about 2 GiB on
GitHub runners and exhaust the heap during the full production `vue-tsc` build.
The fixed budget applies to local and CI containers and cannot be replaced by
host `NODE_OPTIONS`. It is a per-process old-space limit, not a total RSS cap;
the 8-GiB container limit and existing heavy-step serialization remain in force.
WSL local full checks use disposable exact-commit Linux checkouts to avoid Windows
bind-mount source I/O; direct-bind diagnosis remains available. Local and CI checks
share streaming output, report mounts and scoped cleanup. See
[validation operations](VALIDATION_OPERATIONS.md) for lifecycle, benchmarks,
network handling and evidence retention.

Colima runtime regressions verify profile/socket/context selection, resource
requirements, Buildx driver/node binding, profile-isolated dependency cleanup,
scoped Docker/Buildx operations and forbidden fallbacks. Deployment
checks cover maintained Compose security/resources and actual Redis probes.
Mocked runtime tests do not establish live platform integration; record an
actual launcher smoke result separately.

Redis health regressions also execute the actual deployment probe commands
against test-owned foreground Redis servers on private Unix sockets. The image
includes `redis-server` and `redis-tools` for this coverage. No live Redis or
TCP port is used; every child is terminated and waited for before removing its
temporary directory. Compose credential bindings are checked with stale
inherited authentication before executing the real Redis health probes.

Docker integration runs on a dedicated ephemeral GitHub Linux VM. Only that
validation lane may mount the VM's Docker socket and matching CLI and use host networking
for testcontainers ports. It has read-only GitHub permissions, no publication
or proof-producer credentials, and no persisted checkout credentials. The socket
grants control over the candidate VM; privileged result production must never
share that VM. Testcontainers and one-shot cleanup remain scoped to the job;
no global prune or live deployment services are used.
Release image packaging uses the socket only in its separate publisher or
read-only rehearsal VM; publisher credentials never enter candidate validation.

The classification action also runs its deterministic-tree checks inside the
validation container. It stages output in a temporary mounted file, copies
metadata to GitHub outputs, and removes the temporary file on success/failure.

## Focused release finalization

Only an independently regenerated deterministic tree for an already verified
published tag may use `release-finalization`. Published tag, Release-workflow
and immutable-asset queries remain mandatory. Repository policy then runs the
focused published mapping check inside the container. Full application,
frontend and vulnerability suites are omitted for this verified profile.
Ordinary release preparation remains full-profile. Preparing/reusing a
toolchain image is not repeating the application matrix.

CI classifies once in `repository-policy`, which verifies the published Release,
immutable assets and focused mapping. The other full CI jobs depend on its
successful `full` output and are skipped before allocating runners/containers
for finalization. Security Scan classifies once in `backend-security`; its
vulnerability scan runs only for `full`, and `frontend-security` starts only
after that job succeeds with `full`. Ordinary PRs and scheduled security scans
retain the complete applicable suites.

Existing required status contexts remain unchanged. Finalization promotion
requires `sub2api/local-validation`, `repository-policy`, `test` and
`backend-security` to pass. Only `deployment-config`, `frontend`,
`golangci-lint`, `goreleaser-config` and `frontend-security` may report `SKIPPED`.
Failed, cancelled, missing or unknown skipped contexts cannot authorize merge.
These exceptions apply only after independent typed-proof, publication and
deterministic-tree verification. Ordinary promotion rejects every skipped
required check.

Finalization branch commits keep PR workflows enabled. Protected auto-merge
sets the exact subject `docs(release): mark <tag> published [skip ci]`, using
GitHub's [native skip instruction](https://docs.github.com/en/actions/how-tos/manage-workflow-runs/skip-workflow-runs)
to suppress CI and Security Scan on the resulting main push. Completion checks
the actual merge's exact parents, tree, subject and containment in main; it does
not wait for those absent workflows. This exception does not change ordinary
main CI, weekly security scans or the exact-main provenance required for tags.

## CI authority cutover prerequisites

These coverage jobs do not publish local-success or replacement-success
statuses. PR-controlled workflow names alone cannot replace local proof.
Before CI-only submission is activated:

1. Finish coverage/platform accounting and capture successful full/focused CI
   at exact inputs.
2. Establish an approved protected controller/definition, isolated read-only
   candidate jobs and metadata-only privileged result production.
3. Enforce the approved required workflow or exclusively controlled producer
   identity in actual protection. A generic Actions app/check name is insufficient.
4. Bind proof to repo/PR, base/head, tested merge SHA/tree, profile/tag,
   definition/pins, run ID/attempt and all applicable required jobs. Reject
   missing/skipped/failed/foreign/stale evidence.
5. Require the new gate before removing the old one; synchronize CLI proof,
   promotion, repository rules and docs. Submit migration under the old rules.
   Preserve exact actual merged-main CI/Security Scan for release candidates;
   retain the independently verified finalization exception above.

Inspection found an active strict default-branch ruleset with required local,
CI and security contexts and no bypass actors. This is a personal repository
with no configured Actions secrets at inspection. An enforceable exclusive
producer remains a prerequisite, so local full validation remains mandatory.
A maintainer's broad interactive OAuth credential must not be placed in
candidate jobs as a workaround.

## Focused checks

Inside the prescribed validation container:

```bash
python3 tools/test_ci_validation.py
python3 tools/test_release_policy.py
python3 skills/push-cli/tests/test_push_cli.py
```

For host orchestration of a focused local container check:

```bash
python3 tools/ci_validation.py ensure
python3 tools/ci_validation.py exec -- python3 tools/test_ci_validation.py
```

The helper performs no push, PR creation, successful local status or release
publication. Final submission still uses the maintained push-cli entrypoint.
