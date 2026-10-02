# Docker and WSL2 Local Deployment

Read [deploy/README.md](../../../deploy/README.md) and
[DOCKER.md](../../../deploy/DOCKER.md) for Compose configuration, storage,
startup, and backups. Follow the shared
[lifecycle contract](../../../deploy/DEPLOYMENT_LIFECYCLE.md).

## Select the runtime and project

On Linux, use local Docker Engine and Compose v2. On Windows, first inspect
`wsl.exe -l -v`, select an existing running WSL2 Debian/Ubuntu distribution,
and run every Docker, Compose, build, filesystem, and readiness command inside
that distribution. Verify `docker info` and `docker compose version` there.
Never call Windows-host Docker, silently choose another distribution, or fall
back to a host build. Use `wslpath` to translate Windows repository/deployment
paths. Prefer working inside WSL for command sequences; do not pass Linux
multiline shell text as loosely quoted Windows arguments.

Inspect the existing Compose ownership labels before selecting the project
name, files, and deployment directory. Preserve that project identity. Choose
its maintained configuration (`docker-compose.local.yml` for bind directories
or `docker-compose.yml` for named volumes), existing private `.env`, and any
existing overrides. Do not switch storage modes during Web replacement.
The `docker-deploy.sh` helper initializes/downloads deployment files; it is not
an upgrade command and must not overwrite an existing deployment.

Acquire a target-specific lock before preparation and keep it through cleanup.
Record the current Web container's immutable `.Image` value, its owned image
references, and safe mount/port metadata. Project only those fields from
inspection; do not print environment contents or raw resolved Compose output.

## Prepare the image

For current-code deployment, build at the repository root in the selected
Linux/WSL environment:

```bash
deployment_version=$(cat backend/cmd/server/VERSION)
deployment_commit=$(git rev-parse HEAD)
docker build --tag sub2api-plus:local-candidate --file Dockerfile \
  --build-arg "VERSION=${deployment_version}" \
  --build-arg "COMMIT=${deployment_commit}" .
```

Record dirty worktree state rather than treating it as an exact committed
build. For a published-image deployment, `docker pull <requested-reference>`
and inspect its resolved immutable identity. Do not change the current image
reference until preparation succeeds.

Preserve the previous image by its immutable identity and a recovery reference
before moving a mutable tag. For local builds, promote the candidate to the
bounded `sub2api-plus:local-current` reference. For pulled images, deploy the
resolved repository digest when available.

Use a private temporary Compose override to select the prepared application
image, retaining all existing configuration and volumes:

```yaml
services:
  sub2api:
    image: sub2api-plus:local-current
    pull_policy: never
```

For pulled deployments, replace the image value with the prepared reference.
Pass the maintained base file first and this override last with `-f`. Keep the
same `--project-name`, `--project-directory`, and `--env-file` on every Compose
invocation. Preserve the successful image selection in the deployment's
private override/record before removing temporary files, so a later routine
`up` does not silently revert to the base file's `latest` image. On failure,
restore the previous selection. Do not edit tracked Compose defaults.

## Replace and verify

Below, `docker compose` means the selected project's complete command,
including its files, directory, environment file, and project name:

```bash
docker compose config --quiet
docker compose stop sub2api
docker compose rm --force sub2api
docker compose up --detach --no-deps --pull never sub2api
docker compose exec -T sub2api wget -q -T 5 -O /dev/null http://localhost:8080/health
```

Start missing dependencies through the same project before Web replacement
and wait for their readiness. Reuse healthy dependencies; `--no-deps` prevents
the replacement command from recreating them. Explicit removal before `up`
avoids a second transitional Web container. Here `rm --force` suppresses the
confirmation prompt for the already stopped selected service. Do not use
`down`, `down -v`, anonymous-volume renewal, force deletion of running
containers, or global orphan removal.

Bound Web readiness to 180 seconds. Check its running/health state and the
internal probe, then probe the configured published endpoint from Linux or
the selected WSL distribution. If the requested client is Windows, also
verify its published endpoint from Windows before claiming Windows access
works. A container-only check does not prove published-port reachability.

## Cleanup and recovery

After health passes, retain the selected image digest and an explicitly
requested predecessor. Delete owned unused references with
`docker image rm <reference>` after comparing IDs against every container,
including stopped and unrelated containers, and against retained image
aliases. For an owned untagged image, remove its recorded ID only after those
checks. Never use `--force` or a global image/system/builder prune.

If replacement fails, remove the failed Web container through the same
project, restore the previous compatible image selection, and attempt `up`
and health checks once. Keep needed recovery resources if that fails. Remove
temporary override/lock resources while retaining the durable last-healthy
record and successful image selection. Persistent data and shared BuildKit
caches remain intact.
