# Apple container Deployment

Sub2API Plus can run as a native stack with Apple's `container` CLI. This workflow runs the published Sub2API Plus, PostgreSQL, Redis, and optional MinIO OCI images without Docker Desktop or a Docker-compatible daemon.

## Support Level

Apple `container` support is intended for local development and operator-managed deployments on a Mac. Docker Compose remains the recommended production deployment path.

Apple `container` 1.1 does not provide restart policies, automatic startup, workload health scheduling, a Docker API socket, or full Compose orchestration. `apple-container.sh` supplies ordered startup and readiness checks when you invoke it, but it is not a continuously running supervisor.

## Requirements

- A Mac with Apple silicon
- macOS 26 or newer
- Apple `container` 1.1.0 or newer
- `openssl` for generating initial secrets
- Local Network access for `container-runtime-linux` when macOS prompts during the first published-container startup

Install Apple `container` from its [official releases](https://github.com/apple/container/releases), then verify it:

```bash
container --version
```

## Quick Start

```bash
git clone https://github.com/luckykuang/sub2api-plus.git
cd sub2api-plus/deploy

# Creates .env with random PostgreSQL, JWT, and TOTP secrets.
./apple-container.sh init

# Review optional settings before startup.
nano .env

# Creates volumes/network/containers, waits for dependencies, and starts Sub2API Plus.
./apple-container.sh up

# Verifies PostgreSQL, Redis, optional MinIO, and the application endpoint.
./apple-container.sh status
```

Open `http://localhost:8080`. If `ADMIN_PASSWORD` is empty, retrieve the generated password with:

```bash
./apple-container.sh logs app
```

The env file uses literal `KEY=value` syntax. Do not use Compose expressions such as `${VALUE:-default}`, and do not quote values unless the quote characters are part of the intended value. `BIND_HOST` must be an IPv4 address, and `SERVER_PORT` must be between 1025 and 65535.

Keep the generated `TOTP_ENCRYPTION_KEY` unchanged after the first start. It is required not only for persistent TOTP and encrypted backup secrets, but also before an administrator can save a Prompt Audit endpoint token. `SKIP_SETUP` remains `false` by default; set it to `true` only during a controlled recovery when the first-run administrator bootstrap must be bypassed.

Passkey is also opt-in. Set `WEBAUTHN_ENABLED=true`, an exact domain in
`WEBAUTHN_RP_ID`, and a comma-separated origin list in `WEBAUTHN_RP_ORIGINS`,
then run `./apple-container.sh up` to recreate the application container. The
administrator must still enable Passkey in System Settings after the relying
party configuration passes validation. For local HTTP testing, use a localhost
RP ID and origin; non-local deployments require HTTPS origins.

## Commands

Start dependencies and replace the application container. Replacing the
container clears its writable layer and temporary files; `/app/storage`,
PostgreSQL, Redis, MinIO, and host bind directories remain persistent.

```bash
./apple-container.sh up
```

Also replace PostgreSQL, Redis, and enabled MinIO containers while preserving
their persistent storage:

```bash
./apple-container.sh up --recreate
```

Stop containers while preserving all resources and data:

```bash
./apple-container.sh down
```

Restart the stack in dependency order:

```bash
./apple-container.sh restart
```

Show workload state and run live health probes:

```bash
./apple-container.sh status
```

Show Apple Containers disk usage:

```bash
./apple-container.sh disk-usage
```

Follow one service's logs:

```bash
./apple-container.sh logs app -f
```

```bash
./apple-container.sh logs postgres -f
```

```bash
./apple-container.sh logs redis -f
```

```bash
./apple-container.sh logs minio -f
```

Pull and redeploy only the configured Sub2API application image. The current
application image is retained as
`localhost/sub2api-apple-rollback:previous` until a later successful upgrade
replaces it:

```bash
./apple-container.sh upgrade
```

Pull and redeploy the application image, then remove the previous image only
after both application health checks pass:

```bash
./apple-container.sh upgrade --prune-previous-image
```

If an upgrade fails, the rollback image is preserved. Set
`APPLE_CONTAINER_SUB2API_IMAGE=localhost/sub2api-apple-rollback:previous` in
the deployment `.env`, then recreate the application container:

```bash
./apple-container.sh up
```

Pull every configured stack image:

```bash
./apple-container.sh pull
```

Apply PostgreSQL, Redis, or MinIO image changes after the full pull:

```bash
./apple-container.sh up --recreate
```

Globally remove dangling images that are unused by Apple Containers. This can
affect dangling images from other projects and therefore always requires an
explicit command:

```bash
./apple-container.sh cleanup --dangling-images
```

Delete only owned Sub2API named volumes that have been replaced by bind mounts.
The command verifies the live container mount source and destination before it
offers to delete a volume:

```bash
./apple-container.sh cleanup --legacy-volumes
```

Delete containers and the network while preserving named volumes:

```bash
./apple-container.sh destroy --yes
```

Permanently delete the stack and all named-volume data:

```bash
./apple-container.sh destroy --volumes --yes
```

`cleanup --legacy-volumes` never deletes a host bind directory. It refuses to
delete a named volume when the corresponding container is missing, the live
mount does not match `.env`, the volume is not owned by this stack, or any
Apple container still references the volume.
`cleanup --dangling-images` invokes Apple Containers' global dangling-image
prune and does not remove tagged rollback images.

`destroy --volumes` does not remove `.env`, backup files, host bind
directories, or pulled images. Delete credentials and backups separately when
decommissioning a deployment. Use `container image delete <image>` only after
confirming no other Apple containers use that image.

After a host reboot or `container system stop`, run `./apple-container.sh up` again. Apple `container` does not automatically restart persisted containers.

## Disk Lifecycle

Apple Containers gives each running container a separate lightweight VM root
filesystem. `container system df` therefore reports an active container size
even when the files visible inside that container are small. Normal `up`
deletes the old Sub2API container before creating its replacement, so old
writable-layer files do not accumulate, but the replacement allocates a new
runtime filesystem of a similar size. Active container space is not
reclaimable cache.

Image space becomes reclaimable when a pull replaces a mutable tag and the old
image no longer has a tag or container reference. Use the explicit dangling
image cleanup command only after validating the new deployment. A tagged
`localhost/sub2api-apple-rollback:previous` image is intentionally not
dangling.

Named volumes and host bind directories are persistent data, not deployment
cache. `container system df` reports Apple-managed volumes but does not include
the disk usage of arbitrary host bind directories. Inspect and back up those
host paths separately.

The `up` and `upgrade` commands do not invoke `container build`, so they do not
create Apple Builder cache. If an operator separately uses `container build`,
the builder can be stopped without deleting its cache:

```bash
container builder stop
```

Delete the builder and its reusable build cache only when a subsequent full
rebuild is acceptable:

```bash
container builder delete
```

## Configuration

The script uses `deploy/.env`, the same source file used by Docker Compose. Export `SUB2API_ENV_FILE` to use another file for every command in the current shell:

```bash
export SUB2API_ENV_FILE=/absolute/path/to/sub2api.env
./apple-container.sh init
./apple-container.sh up
```

Apple-specific image overrides are available:

```dotenv
APPLE_CONTAINER_SUB2API_IMAGE=ghcr.io/luckykuang/sub2api-plus:latest
APPLE_CONTAINER_SUB2API_BINARY=
APPLE_CONTAINER_SUB2API_RESOURCES_DIR=
APPLE_CONTAINER_POSTGRES_IMAGE=postgres:18-alpine
APPLE_CONTAINER_REDIS_IMAGE=redis:8-alpine
APPLE_CONTAINER_MINIO_IMAGE=pgsty/minio:RELEASE.2026-06-18T00-00-00Z
```

For local secondary development when the Apple Builder is unavailable, build a Linux/arm64 binary on the host and set `APPLE_CONTAINER_SUB2API_BINARY` to its absolute path. During each `up`, the script temporarily compresses and copies that binary into the newly created application container while retaining the configured OCI image as its runtime base. The binary must contain any required embedded frontend assets. When a local binary is configured, the script also copies the repository's `backend/resources` directory into `/app/resources`; set `APPLE_CONTAINER_SUB2API_RESOURCES_DIR` to an absolute alternative resource directory when needed. Leave both settings empty for normal published-image deployments.

### Custom Image Version

Git and GitHub Releases use `vX.Y.Z+custom.NNN`; the application embeds the
same value without the leading `v`. Because OCI image tags do not permit `+`,
the release workflow preserves the leading `v` and replaces only `+` with
`-`. The current mapping is:

```text
Git/GitHub:         v0.1.183+custom.002
Application:        0.1.183+custom.002
Apple/OCI image:    ghcr.io/luckykuang/sub2api-plus:v0.1.183-custom.002
```

Use the following values when building or publishing this OCI image:

```bash
docker build \
  --build-arg VERSION=0.1.183+custom.002 \
  --tag ghcr.io/luckykuang/sub2api-plus:v0.1.183-custom.002 \
  .
```

After that image is available to the Apple `container` runtime, set
`APPLE_CONTAINER_SUB2API_IMAGE=ghcr.io/luckykuang/sub2api-plus:v0.1.183-custom.002`. Until then, keep
the published image as the runtime base and use `APPLE_CONTAINER_SUB2API_BINARY`
for the custom binary.

The normal `up` command recreates the application container, so application environment changes are applied immediately. Use `up --recreate` when changing PostgreSQL, Redis, or MinIO container images or runtime configuration. Persistent data remains in named volumes unless host directories are configured explicitly:

```dotenv
APPLE_CONTAINER_SUB2API_DATA_DIR=/Users/Shared/sub2api-plus/app
APPLE_CONTAINER_POSTGRES_DATA_DIR=/Users/Shared/sub2api-plus/postgres
APPLE_CONTAINER_REDIS_DATA_DIR=/Users/Shared/sub2api-plus/redis
APPLE_CONTAINER_MINIO_DATA_DIR=/Users/Shared/sub2api-plus/minio
```

These settings use Apple Container bind mounts for `/app/storage`,
`/var/lib/postgresql`, `/var/lib/redis`, and `/data`, respectively. The script
creates a missing directory with mode `0700`; it never deletes or clears a host
directory. If a setting is empty, that service falls back to its managed named
volume. Keep the deployment `.env` (mode `0600`) in a separately backed-up
configuration directory, for example `/Users/Shared/sub2api-plus/config/.env`,
and invoke the script with `SUB2API_ENV_FILE` pointing to that file.

`POSTGRES_USER`, `POSTGRES_PASSWORD`, and `POSTGRES_DB` are applied only when PostgreSQL initializes an empty data volume. Changing them in `.env` and recreating the container does not change an existing database. Rotate a password with `ALTER ROLE`, and plan explicit migrations for user or database changes. To intentionally initialize a new empty database, first back up the old one and use `destroy --volumes`.

Apple-specific handling of shared settings:

| Setting | Apple workflow behavior |
|---|---|
| Application and gateway variables | Passed to Sub2API Plus from `.env` |
| `BIND_HOST`, `SERVER_PORT` | Used for the macOS published port |
| `POSTGRES_USER`, `POSTGRES_PASSWORD`, `POSTGRES_DB` | PostgreSQL first initialization only |
| `REDIS_PASSWORD` | Applied to Redis and Sub2API Plus |
| `DATABASE_PORT`, `REDIS_PORT` | Internal ports are fixed to 5432 and 6379 |
| `POSTGRES_MAX_*`, `REDIS_MAXCLIENTS` | Not currently applied to the database/cache server |
| `APPLE_CONTAINER_SUB2API_DATA_DIR` | Host bind mount for `/app/storage`; empty uses a named volume |
| `APPLE_CONTAINER_POSTGRES_DATA_DIR` | Host bind mount for `/var/lib/postgresql`; empty uses a named volume |
| `APPLE_CONTAINER_REDIS_DATA_DIR` | Host bind mount for `/var/lib/redis`; empty uses a named volume |
| `APPLE_CONTAINER_MINIO_DATA_DIR` | Host bind mount for `/data`; empty uses a named volume |

### Local MinIO for Async Images

Set `MINIO_ENABLED=true` in `deploy/.env` to run MinIO as part of this stack. On the first `up`, the script writes a default `MINIO_ROOT_USER` when needed and generates a random `MINIO_ROOT_PASSWORD` if it is empty. The env file must remain mode `0600`.

```dotenv
MINIO_ENABLED=true
APPLE_CONTAINER_MINIO_IMAGE=pgsty/minio:RELEASE.2026-06-18T00-00-00Z
MINIO_BIND_HOST=127.0.0.1
MINIO_API_PORT=9000
MINIO_CONSOLE_PORT=9001
MINIO_ROOT_USER=sub2api-minio
MINIO_ROOT_PASSWORD=
MINIO_BUCKET=sub2api-images
MINIO_REGION=us-east-1
```

The script publishes the MinIO S3 API at `http://127.0.0.1:9000` and the console at `http://127.0.0.1:9001` by default. It creates `MINIO_BUCKET`, grants anonymous download access to that bucket only, then injects `IMAGE_STORAGE_*` settings into the Sub2API Plus container. This lets asynchronous image-result URLs render directly in a local browser while preserving authenticated writes. Keep `MINIO_BIND_HOST=127.0.0.1` for a local-only deployment. If it is changed to a LAN address, that address is used in generated public image URLs.

The published image API URL is intended for generated image objects only. Do not use this bucket for database backups, credentials, or other private data.

## Managed Resources

The script creates only resources carrying the `org.sub2api.stack=apple-container` label:

| Type | Names |
|---|---|
| Containers | `sub2api-apple`, `sub2api-apple-postgres`, `sub2api-apple-redis`, `sub2api-apple-minio` |
| Network | `sub2api-apple` |
| Volumes | `sub2api-apple-data`, `sub2api-apple-postgres-data`, `sub2api-apple-redis-data`, `sub2api-apple-minio-data` |

The PostgreSQL volume is mounted at `/var/lib/postgresql`, retaining PostgreSQL 18's default child data directory. Sub2API Plus and Redis also store data in child directories below their Apple volume mount points. This is required because Apple named volumes do not have Docker's copy-up and mount-point ownership behavior.

## Networking

Apple `container` 1.1 does not provide Compose-style network-scoped service aliases. After PostgreSQL and Redis start, the script reads their current private-network IPv4 addresses from `container inspect`, injects those addresses into a newly created application container, and then starts Sub2API Plus. The script does not modify `~/.config/container/config.toml` or the macOS host resolver.

Sub2API Plus, PostgreSQL, Redis, and enabled MinIO attach to the private `sub2api-apple` network. PostgreSQL and Redis ports remain unpublished. When enabled, MinIO explicitly publishes its local S3 API and console ports; both default to loopback-only bindings.

The application container is intentionally deleted and recreated by every
`up`, `restart`, and `upgrade` operation because dependency VM addresses can
change after they stop. This replacement clears the old container writable
layer and temporary files. Application data remains in the configured bind
directory or `sub2api-apple-data`; deployment never clears that persistent
storage.

The script checks the published `/health` endpoint from macOS before reporting success. Approve the Local Network prompt on first startup. If the internal probe succeeds but the host-port probe fails with a connection reset, enable Local Network access for `container-runtime-linux`, run `container system stop` followed by `container system start`, and then run `up` again. Runtime upgrades may prompt for permission again.

## Backup and Upgrade

Pin image release tags or digests in `.env` before using this workflow for persistent data. Before an application or database image upgrade, create backups while the stack is healthy:

```bash
umask 077
mkdir -p backups

# Logical PostgreSQL backup.
container exec sub2api-apple sh -c \
  'PGPASSWORD="$DATABASE_PASSWORD" pg_dump -h "$DATABASE_HOST" -U "$DATABASE_USER" "$DATABASE_DBNAME"' \
  > backups/sub2api.sql

# Application configuration and local files.
container exec sub2api-apple sh -c 'tar -C "$DATA_DIR" -czf - .' \
  > backups/sub2api-data.tar.gz

./apple-container.sh upgrade
```

```bash
./apple-container.sh status
```

`upgrade` changes only the Sub2API application image. Use `pull` followed by
`up --recreate` only when intentionally updating PostgreSQL, Redis, or MinIO.
Database migrations are forward-only. Keep the rollback image and both backups
until the upgraded stack has been validated; image rollback alone cannot
reverse a migrated database. Test restore procedures before relying on this
workflow for important data.

To restore these backups into an existing stack, first ensure the image versions are compatible with the backup, then stop writers and replace both data sets:

```bash
# Ensure empty/current resources exist, then stop the stack.
./apple-container.sh up
./apple-container.sh down

# Remove only the app container so a helper can mount its named volume.
container delete sub2api-apple
SUB2API_IMAGE=ghcr.io/luckykuang/sub2api-plus:latest # Match APPLE_CONTAINER_SUB2API_IMAGE in .env.
container run --rm --name sub2api-apple-data-restore \
  --entrypoint /bin/sh \
  --volume sub2api-apple-data:/restore \
  --volume "$PWD/backups:/backup:ro" \
  "$SUB2API_IMAGE" \
  -c 'rm -rf /restore/data && mkdir -p /restore/data && tar -xzf /backup/sub2api-data.tar.gz -C /restore/data'

# Restore the logical database while the application is absent.
container start sub2api-apple-postgres
until container exec sub2api-apple-postgres sh -c 'pg_isready -U "$POSTGRES_USER" -d "$POSTGRES_DB"'; do sleep 1; done
container copy backups/sub2api.sql sub2api-apple-postgres:/tmp/sub2api.sql
container exec sub2api-apple-postgres sh -c '
  export PGPASSWORD="$POSTGRES_PASSWORD"
  dropdb -h 127.0.0.1 -U "$POSTGRES_USER" --if-exists --force "$POSTGRES_DB"
  createdb -h 127.0.0.1 -U "$POSTGRES_USER" "$POSTGRES_DB"
  psql -h 127.0.0.1 -U "$POSTGRES_USER" -d "$POSTGRES_DB" -v ON_ERROR_STOP=1 -f /tmp/sub2api.sql
  rm /tmp/sub2api.sql
'

./apple-container.sh up
./apple-container.sh status
```

For disaster recovery after deleting the named volumes, run `up` once to create a fresh stack before following the restore sequence. Perform restore drills with non-production data first.

To upgrade the Apple runtime itself:

```bash
./apple-container.sh down
container system stop
# Install/update Apple container 1.1.0 or newer.
container system start
./apple-container.sh up
```

## Operational Limitations

- There is no `restart: unless-stopped` equivalent. Run `up` after reboot, or add your own launchd supervisor.
- Health probes run during `up`, `restart`, and `status`; Apple `container` does not continuously schedule them.
- Docker Compose, Testcontainers, Buildx, and tools requiring `/var/run/docker.sock` cannot use this runtime directly.
- Named volume backup and restore must be tested before using this workflow for important data.
- The script targets native `linux/arm64` images. The normal Sub2API Plus release publishes an arm64 variant.
- Runtime environment values, including credentials, are retained in Apple container configuration and are visible to users who can inspect the local runtime.
