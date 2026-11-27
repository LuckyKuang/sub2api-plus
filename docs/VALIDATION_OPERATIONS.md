# Validation operations

The full local gate and exact base/head proof remain mandatory. Use focused
container checks while editing, settle UI behavior, commit the final candidate,
then run `skills/push-cli/scripts/push_cli.py submit-pr` once on that candidate.
A new head or default-branch base requires a new full proof. CI may run alongside
the local matrix for the same candidate; neither result substitutes for the other.

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

## Source storage and I/O evidence

WSL full validation defaults to a fresh Git checkout on the container's Linux
filesystem. It requires clean source and the exact recorded head; the exact base
must be readable in its object store. The clone shares source objects read-only,
but owns its index, refs, source and build outputs. The frozen Linux dependency
generation remains reusable. Checkout conversion follows the source repository's
Git settings to preserve byte parity and cache identities on Windows.
Post-matrix host worktree/base/head checks still run.
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
storage so session restarts do not discard them. Manual clean removes this root
and the historical `/tmp/sub2api-validation-cache` location; ordinary cleanup
retains only the current home generation and removes the historical temporary root.
It also prunes precisely identified BuildKit record IDs rooted in the validation
Dockerfile's `/etc/sub2api-validation` marker, with its installation ancestors and
known browser/package-manager/workspace continuation. Generic base-image records
and unrelated descendants are retained. Unknown ownership is not permission to
delete a record. Apple Builder's shared cache remains outside automatic cleanup.

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
