# Apple Containers Local Deployment

Read [APPLE_CONTAINER.md](../../../deploy/APPLE_CONTAINER.md) for prerequisites,
private environment settings, persistent data, backups, and native runtime
limitations. Follow the shared
[lifecycle contract](../../../deploy/DEPLOYMENT_LIFECYCLE.md).

## Inspect and prepare

Run `container --version`, then `container list --all --format json` and
`container image list --format json`, projecting only the safe fields needed
for ownership and digest matching. Use `container system df` for disk usage.
Do not print raw inspect/list JSON containing process environments.

Use the existing `SUB2API_ENV_FILE` or `deploy/.env`. Reuse its data paths and
credentials; do not run deployment-file initialization over an existing setup.
The existing script checks ownership and locks its mutations. Acquire a
target-specific outer lock before image preparation so preparation, the script
invocation, and cleanup cannot race another deploy-cli operation.

Inspect any existing `APPLE_CONTAINER_SUB2API_BINARY` setting before image
preparation. When an image deployment is requested, clear the overriding
binary/resource settings in the selected private environment file before
replacement and save their previous values for recovery. An explicitly
requested local-binary deployment instead follows `APPLE_CONTAINER.md` and
reports the actual binary source; compile that binary inside the platform
validation container, never on the host.

## Current-code deployment

Build at the repository root. The following metadata reads are host
orchestration; the Go and frontend builds execute inside Apple Builder:

```bash
deployment_version=$(cat backend/cmd/server/VERSION)
deployment_commit=$(git rev-parse HEAD)
container build --platform linux/arm64 \
  --build-arg "VERSION=${deployment_version}" \
  --build-arg "COMMIT=${deployment_commit}" \
  --tag sub2api-plus:local-candidate --file Dockerfile .
```

Record the source worktree state as well as the commit; a dirty tree is not an
exact committed build. Reuse a ready Builder. If it needs preparation, use the
documented memory allocation in `APPLE_CONTAINER.md`. Do not delete a shared
Builder or build a replacement binary with host Go/Node.

Keep the old image addressable by its recorded digest/recovery reference before
changing `sub2api-plus:local-current`. After candidate preparation, tag the
candidate as `sub2api-plus:local-current` and set only
`APPLE_CONTAINER_SUB2API_IMAGE` in the selected private environment file to
that reference. Preserve the old image setting for recovery.

Run the maintained script from the repository root:

```bash
bash deploy/apple-container.sh up
bash deploy/apple-container.sh status
```

For an environment file outside `deploy/.env`, pass its existing absolute path
through `SUB2API_ENV_FILE` to both commands. `up` prepares dependencies, removes
the old Web container, creates its replacement, and verifies both internal and
host-port health. Preserve healthy dependencies; do not add `--recreate` unless
their replacement is part of the request. Its readiness loops have bounded
attempt counts and probe timeouts; do not describe those attempt counts as a
strict wall-clock deadline.

## Published-image deployment

Set the requested image in the selected private environment file and run:

```bash
bash deploy/apple-container.sh upgrade --prune-previous-image
bash deploy/apple-container.sh status
```

This pulls the application image before replacement and prunes the immediate
predecessor after successful health checks. For a requested rollback retention
policy, use `upgrade` without `--prune-previous-image`. Do not use `upgrade`
for an unpublished local build: it always attempts a registry pull.

## Cleanup and recovery

The script's upgrade option is not a sweep of every old `local-*` tag. After
success, enumerate the owned deployment record, check all container image
references and digests, and delete each obsolete unused reference with
`container image delete <reference>`. Keep the current reference and any
requested rollback digest. Remove temporary candidate/recovery aliases only
when safe; do not delete snapshot directories directly.

On failure, restore the previous image reference and the saved image setting
before one attempt at `up`. Preserve data and report both the deployment error
and recovery result. If the script pruned the predecessor after success, do
not promise that an older image remains available for recovery.
