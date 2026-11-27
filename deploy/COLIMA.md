# macOS Colima deployment

macOS builds, deployments and validation use Colima with the Docker runtime and
Docker Compose. The same Compose services, images, persistent storage and health
checks apply on macOS, WSL2 and Linux. Image/version mappings are maintained in
[DOCKER.md](DOCKER.md); use the source version in `backend/cmd/server/VERSION`
when building current code.

## Prepare and verify the runtime

Install Colima, Docker CLI, Compose and Buildx from their official distributions.
The runtime must support `colima status --json`, explicit Docker contexts and
the Docker Buildx builder associated with a context. See the official
[Colima commands](https://colima.run/docs/commands/) and
[Docker contexts](https://docs.docker.com/engine/manage-resources/contexts/).

For the default Colima profile:

```bash
colima start --runtime docker --cpus 4 --memory 10 --activate=false
colima --profile default status --json
docker --context colima context inspect colima --format '{{.Endpoints.docker.Host}}'
docker --context colima info
docker --context colima compose version
docker --context colima buildx inspect colima
```

Compare the context endpoint with `docker_socket` from Colima's status; both must
identify the same local Unix socket. Require `runtime: docker`. Buildx inspection
must report the `docker` driver and exactly one running node whose endpoint is
that context/socket; reject a stored builder with the same name and a different
target. The four-CPU,
ten-GiB VM accommodates the four-CPU/eight-GiB validation container and VM
services. Keep the existing profile configuration and mounts when restarting.
The worktree, linked-worktree Git metadata, dependency caches, reports and
deployment bind directories must be shared with the VM and writable where
needed. Colima's default home mount covers the usual repository/cache locations;
configure extra mounts explicitly for paths outside it before launching checks.
A named profile uses Docker context `colima-<profile>`; the default profile uses
`colima`. `SUB2API_COLIMA_PROFILE` selects the validation profile, defaulting to
`default`. Select that same profile/context explicitly for deployment commands.
The launcher does not start, stop or resize Colima automatically.

Use `--context` on every Docker operation and `--builder <context>` for builds
and Buildx cache inspection/cleanup. These explicit selections keep global
Docker settings and environment-selected remote daemons/builders from changing
the target. `--activate=false` preserves the current global Docker context.
Only project-owned resources may be cleaned up; keep other projects and shared
base-image/build metadata intact.
For the development Compose build, run `compose build --builder <context>`
first, then `compose up --no-build` with the same project/files. This keeps the
builder explicit when creating a container from locally built code.

## Deploy with Compose

Follow [DOCKER.md](DOCKER.md) for a new installation and the shared
[deployment lifecycle](DEPLOYMENT_LIFECYCLE.md) for preparation, replacement,
recovery and image retention. Use the existing private environment file, Compose
project identity and storage mode for an established deployment. The
[deployment skill](../skills/deploy-cli/references/colima.md) describes the
current-code and published-image paths.

For a new deployment, prepare the private `.env` and maintained Compose file as
in the Docker guide, then use the explicit Colima context:

```bash
docker --context colima compose --env-file .env -f docker-compose.yml config --quiet
docker --context colima compose --env-file .env -f docker-compose.yml up --detach
```

Keep project, directory, environment and override arguments identical on every
invocation. Use a private override for a locally built or explicitly pinned
application image. Do not replace an existing deployment's files with the
initialization helper. Preserve database, Redis, application and object-storage
data. Configure an existing S3-compatible service through the documented
application storage settings when asynchronous image storage is needed.

## Build current code

From the authoritative repository worktree, prepare the candidate before
replacing the running Web container:

```bash
deployment_version=$(cat backend/cmd/server/VERSION)
deployment_commit=$(git rev-parse HEAD)
docker --context colima build --builder colima --tag sub2api-plus:local-candidate \
  --file Dockerfile --build-arg "VERSION=${deployment_version}" \
  --build-arg "COMMIT=${deployment_commit}" .
```

Go and frontend builds execute in the Docker build stages. Record uncommitted
source changes when present; the commit alone does not describe a dirty build.
Follow the shared lifecycle to preserve recovery resources, select the candidate
in the private override and replace only the application service.

## Readiness, restart and storage

Verify the internal `/health` endpoint and the configured host-published endpoint
within the deployment readiness budget. Docker health alone does not prove that
the host port is reachable. Restart policies operate while the Colima VM is
running. After a host restart, start the selected Colima profile and verify
readiness again; this guide does not install a host startup service.

A runtime change does not migrate existing data. Before moving an established
stack into Colima, record its source image and storage mappings, take verified
backups and plan the database/object-storage cutover. Restore into explicit
Colima mounts or volumes and verify the application before retiring the source.
Changing repository instructions does not authorize stopping or deleting a
running deployment or its data.

## Validation and troubleshooting

Use the maintained launcher and cleanup rules in
[validation operations](../docs/VALIDATION_OPERATIONS.md#macos-colima).
Deployment smoke checks do not publish repository-validation proof.

If a cold image build cannot resolve its registry, inspect DNS inside the Colima
VM and the resolver provided by the host. Repair the selected profile's DNS
configuration with the documented `colima start --dns` option and verify that
its guest resolver file points to an existing service/file. Keep workstation
resolver addresses out of tracked project configuration. Diagnose download
origins without printing proxy credentials or full runtime inspection output.
