# Validation operations

The full local gate and exact base/head proof remain mandatory. Use focused
container checks while editing, settle UI behavior, commit the final candidate,
then run `skills/push-cli/scripts/push_cli.py submit-pr` once on that candidate.
A new head or default-branch base requires a new full proof. CI may run alongside
the local matrix for the same candidate; neither result substitutes for the other.

## macOS Colima

macOS validation and deployment use Colima with the Docker runtime, Compose and
Buildx. Prepare the runtime as described in [the Colima guide](../deploy/COLIMA.md).
Use at least four CPUs and ten GiB VM memory; the validation container keeps its
existing four-CPU/eight-GiB limit, Go memory target and Node heap budget.

```bash
colima start --runtime docker --cpus 4 --memory 10 --activate=false
python3 skills/push-cli/scripts/push_cli.py ensure
```

`SUB2API_COLIMA_PROFILE` selects a profile (default `default`, including an empty
value). The launcher reads `colima --profile <profile> status --json`, requires a
running Docker runtime and sufficient VM resources, and checks the profile's
`docker_socket` against its Docker context endpoint. Context names follow
Colima's [profile convention](https://colima.run/docs/profiles/): `colima` for
`default`, `colima-<profile>` otherwise. Every image, run, Compose and cleanup
operation uses explicit `docker --context <context>`; builds and Buildx cache
operations select the matching builder explicitly. Global Docker context,
`DOCKER_HOST`, `DOCKER_CONTEXT`, `BUILDX_BUILDER` and `BUILDKIT_HOST` cannot redirect
these operations. The launcher neither changes the global context nor starts,
resizes or stops a profile. An unavailable/wrong runtime, mismatched endpoint or
missing Compose/Buildx is a hard failure. Buildx must use the `docker` driver
with exactly one running node on that context/socket; a stored builder sharing
the context name cannot substitute a foreign endpoint.

For a named profile, start it explicitly and bind the same name to the launcher:

```bash
colima start validation --runtime docker --cpus 4 --memory 10 --activate=false
SUB2API_COLIMA_PROFILE=validation python3 skills/push-cli/scripts/push_cli.py ensure
```

For isolated PostgreSQL/Redis fixtures, create a project-owned Docker network in
that context and pass its name as `SUB2API_VALIDATION_DOCKER_NETWORK`. The
launcher attaches the validation container to it; forward fixture DSNs and
Redis bindings by environment name as usual. The variable defaults to unset
(the Docker default network), accepts a single project-owned network name and cannot be
combined with `SUB2API_VALIDATION_WSL_NETWORK`. Remove owned fixture services
and their network after each attempt. Never use a running application's data
or print full Docker/Compose inspection output. Reserved `host`, `none` and
`bridge` values are rejected; WSL host networking keeps its dedicated option.

## WSL lifecycle and arguments

On Windows, the launcher selects only installed WSL2 Debian/Ubuntu distributions,
preferring a running distribution. Invoking a stopped supported distribution
starts it and allows a bounded Docker-readiness retry. Docker Engine and Compose
must be available there; the launcher never starts host Docker or installs a
daemon automatically.

An owned foreground `cat` process with piped stdin holds the selected WSL session
for the action. Success, failure and interruption close stdin and join the child.
No detached sleep, user startup task, systemd change or global WSL setting is
needed. This protects a validation action, not an unattended business deployment.
Use `wsl --exec` argument arrays so the default shell cannot reinterpret paths,
regular expressions or parentheses. WSL UTF-16 output is decoded before parsing.
Launcher output uses UTF-8 so Unicode tool progress cannot fail on a Windows
legacy code page. CI helper invocations own the same foreground WSL session.
Host-launched CI lanes enter `tools/ci_validation.py` relative to the mounted
repository working directory; Windows drive paths never become Linux script
arguments. This keeps focused policy lanes available through the same container
boundary as local submission.

## Source storage and I/O evidence

WSL full validation defaults to a fresh Git checkout on the container's Linux
filesystem. It requires clean source and the exact recorded head; the exact base
must be readable in its object store. Git probes disable optional index refresh
locks so interrupted source reads cannot leave a host index lock. Mandatory
locks for writes in the disposable clone remain enabled.
The clone shares source objects read-only,
but owns its index, refs, source and build outputs. The frozen Linux dependency
generation remains reusable. Checkout conversion follows the source repository's
Git settings to preserve byte parity and cache identities on Windows.
Post-matrix host worktree/base/head checks still run.
The private checkout path depends only on the resolved source location, preserving
Go build-cache identity across disposable containers without changing compiler
flags. Each invocation creates the directory exclusively; an existing active or
stale path is rejected, never reused or removed by a competing invocation. Separate
containers have isolated writable layers at the same path.
The checkout is removed in `finally` and lives in the `--rm` container writable
layer, so a stopped-container sweep also reclaims interrupted snapshots.

`check --wsl-workspace bind` retains direct-bind execution for diagnosis. The same
full matrix, resource limits and proof rules apply to both storage modes. macOS
and Linux continue to use their platform runtime and ordinary source binding.

For an isolated filesystem comparison, run this **inside the pinned validation
container**, with the clean Windows source mounted as `/workspace` and the same
four-CPU/eight-GiB limits:

```bash
python3 tools/validation_workspace.py benchmark --source /workspace \
  --head "$(git rev-parse HEAD)" --base "$(git rev-parse origin/main)" \
  --report /tmp/sub2api-validation-report/io.json
```

Mount `/tmp/sub2api-validation-report` to the persistent report directory if the
comparison must survive container deletion. The comparison reads the same tracked
bytes in both locations and alternates order for three paired rounds. An initial
parity pass reads both trees, synchronizes only LF/CRLF representation differences
in the disposable benchmark tree, and rejects other differences. Each measured
pair must have the same content digest. The report records preparation time and
the number of representation corrections. It measures repeated file reads, not clean OS
caches, application speed, network throughput or IOPS. Do not flush global OS
caches or extrapolate this ratio into an end-to-end speed claim. Compare full
matrix summaries for the latter, retaining toolchain, head, limits and dependency
cache state. Microsoft's [filesystem guidance](https://learn.microsoft.com/en-us/windows/wsl/filesystems)
explains why a Linux checkout is the candidate to measure.

## Progress and retained evidence

Parallel checks stream output as it arrives and emit a twenty-second heartbeat
while quiet. Their bounded in-memory tail does not accumulate the full suite log.
The full local matrix and CI lanes write atomic JSON summaries after each step,
and finish with success/failure/interruption status. Summaries include exact Git
metadata, image/cache generation, dependency-cache initial state, step duration,
heavy-slot waiting, exit code and whole-container cgroup peak/limit/CPU counters.
Container counters are aggregate, not per-step RSS or CPU measurements.

The seven-provider Python gates execute Go tests and acquire the same memory
slot as the full Go suites, lint and frontend production build. Their package
deadlines and required-case checks follow the
[seven-provider matrix](SEVEN_PROVIDER_TEST_MATRIX.md#verification-and-remaining-boundaries).

Host reports live in `~/.local/state/sub2api-validation/reports`; on Windows this
is the selected WSL user's home. They mount at `/tmp/sub2api-validation-report`.
These private files contain no command arguments, environment values, raw test
output, credentials or user payloads. They are diagnostic evidence, never a
substitute for the typed PR proof. Dependency-cache cleanup deliberately retains
them. GitHub run/job logs remain the remote evidence for runner preparation and
cache restore times; a cache-generation match does not prove a cache hit.

Layout browser traces are disabled by default. For disposable fixture diagnosis,
pass `--trace retain-on-failure` to `pnpm --dir frontend run test:layout` inside
the validation container and set `SUB2API_LAYOUT_OUTPUT_DIR` to a separately
mounted evidence directory. Review/redact traces before sharing; they may capture
page content and network data. The authenticated e2e suite keeps its own artifact
privacy policy and is not covered by this layout-only command.

## Network and image preparation

Compose configuration checks run inside the pinned image, using the official
binary pinned in `.tool-versions` and all four maintained Compose templates.
Only the tracked `.env.example` is loaded, with inert external-service values
for the standalone template; the parser receives no Docker socket
or private deployment environment. Host-side Compose parsing is not a validation
exception. Deployments continue to use the selected runtime's `docker compose`.

Reuse an image only when its deterministic toolchain/Dockerfile identity matches.
Missing images require downloads. The pinned lint binary comes from its official
versioned GitHub archive; a failed download stops the install before the completion
marker, and the installed binary must report its version. Do not continue with an
incomplete toolchain after a network failure.

Explicit `HTTP_PROXY`, `HTTPS_PROXY`, `NO_PROXY` and lowercase variants pass by name
to WSL, Docker build's predefined proxy arguments and validation containers. Values
never enter displayed arguments or Dockerfile `ENV`. For an existing proxy reached
through the WSL host network, set `SUB2API_VALIDATION_WSL_NETWORK=host` in the current
launcher process; this applies to image builds and validation runs. The default
network remains unchanged. Do not persist a workstation address in project config.
Before a cold build, check proxy listener/reachability and DNS/TLS for the failing
download origin. Record only host, stage, elapsed time and error category, without
proxy credentials. A successful probe does not guarantee the subsequent transfer.

## Scoped cleanup

`push-cli clean --yes` first passes the authenticated repository gate, then stops
and removes only labelled validation containers before deleting their images.
WSL dependencies live in `~/.cache/sub2api-validation/<generation>` on Linux
storage so session restarts do not discard them. The default Colima profile
uses the same path under the macOS home. Named Colima profiles use
`~/.cache/sub2api-validation-colima-<profile>/<generation>` so another profile's
cleanup cannot delete caches mounted by an active validation. Image, container,
BuildKit and dependency cleanup affect only the selected profile; switching
profiles does not clean another VM's resources. The launcher prepares all Go,
pnpm and node_modules mounts through `cache_mounts`; the command builder
consumes that supplied list and does not create an implicit dependency overlay.
Cache and home-resolution test fixtures use private temporary homes. Test a
missing-home fallback separately without writing into the real image home:
package-manager resources there can belong to root while CI runs as the runner
UID. For changes to these fixtures, run the affected policy checks in the pinned
validation image as both the usual local user and an unprivileged user (for
example UID/GID 1001). A local root pass does not
establish that filesystem permissions work in CI.
The launcher explicitly sets
`npm_config_store_dir` to the mounted `$PNPM_HOME/store`; otherwise pnpm can select
the Windows source disk and copy packages across filesystems. Cache identity
includes the layout version as well as toolchain and dependency inputs.
The pinned pnpm 9 frozen installation runs with
`--config.confirmModulesPurge=false` so an incompatible mounted/symlinked
modules directory is actually rebuilt without a closed-stdin confirmation prompt.
Frozen lockfile enforcement remains mandatory and is exercised against a changed
dependency manifest in the offline installation regression.
Manual clean removes all home generations and the historical temporary root.
Ordinary cleanup retains the current home generation and removes the historical
`/tmp/sub2api-validation-cache` root.
It also prunes precisely identified BuildKit record IDs rooted in the validation
Dockerfile's `/etc/sub2api-validation` marker, with its installation ancestors and
known browser/package-manager/Compose/workspace continuation. Generic base-image records
and unrelated descendants are retained. Records shared with retained images also
keep their reusable build metadata; pruning them would not reclaim layer storage.
See Docker's [Shared record semantics](https://docs.docker.com/reference/cli/docker/buildx/du/).
Unknown ownership is not permission to
delete a record. Colima inspection/pruning stays on the verified context and
its matching builder; records from other projects remain outside ownership.

Ordinary cleanup defers all deletion while another labelled validation is running.
Otherwise it preserves the current image/dependency generation, removes stopped
labelled containers before stale images/caches, and prunes owned BuildKit records.
Manual full clean deletes the current generation too, so the next run is cold.
Never use global system/image/builder/volume prune. Docker cache and image sizes
overlap; measure actual reclaimed filesystem space rather than adding them.

Deleting Linux files makes space reusable inside WSL. VHDX compaction and recovery
of Windows host allocation are a separate maintenance operation requiring a
business shutdown window; cache cleanup does not silently stop a business stack.

## PR success ordering

After the full matrix, recheck exact source/base/head, push the exact branch,
create/update its typed PR marker, read back exact base/head/marker, and only then
publish `sub2api/local-validation=success`. Success is the final GitHub mutation;
auto-merge may act immediately afterward. Failure writing/verifying the PR leaves
success unpublished. Multiline PR bodies use temporary payload files which are
deleted after the request, so body contents are not printed in command logs.
