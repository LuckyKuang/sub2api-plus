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
cd sub2api/deploy

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

## Commands

```bash
# Start dependencies and recreate the lightweight app container with current IPs.
./apple-container.sh up

# Also recreate PostgreSQL, Redis, and enabled MinIO containers, preserving volumes.
./apple-container.sh up --recreate

# Stop containers while preserving all resources and data.
./apple-container.sh down

# Restart PostgreSQL, Redis, and Sub2API Plus in dependency order.
./apple-container.sh restart

# Show resource state and run live health probes.
./apple-container.sh status

# Follow one service's logs.
./apple-container.sh logs app -f
./apple-container.sh logs postgres -f
./apple-container.sh logs redis -f
./apple-container.sh logs minio -f

# Pull all configured images for linux/arm64, then recreate containers.
./apple-container.sh pull
./apple-container.sh up --recreate

# Delete containers and the network, preserving named volumes.
./apple-container.sh destroy --yes

# Permanently delete the stack and all application/database/cache data.
./apple-container.sh destroy --volumes --yes
```

`destroy --volumes` does not remove `.env`, backup files, or pulled images. Delete credentials and backups separately when decommissioning a deployment. Use `container image delete <image>` only after confirming no other Apple containers use that image.

After a host reboot or `container system stop`, run `./apple-container.sh up` again. Apple `container` does not automatically restart persisted containers.

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
APPLE_CONTAINER_POSTGRES_IMAGE=postgres:18-alpine
APPLE_CONTAINER_REDIS_IMAGE=redis:8-alpine
APPLE_CONTAINER_MINIO_IMAGE=pgsty/minio:RELEASE.2026-06-18T00-00-00Z
```

For local secondary development when the Apple Builder is unavailable, build a Linux/arm64 binary on the host and set `APPLE_CONTAINER_SUB2API_BINARY` to its absolute path. During each `up`, the script temporarily compresses and copies that binary into the newly created application container while retaining the configured OCI image as its runtime base. The binary must contain any required embedded frontend assets. Leave the setting empty for normal published-image deployments.

### Custom Image Version

The application version for this custom branch is `v0.1.166+custom.002`.
Use `ghcr.io/luckykuang/sub2api-plus:v0.1.166-custom.002` when building or
publishing an OCI image:

```bash
docker build \
  --build-arg VERSION=0.1.166+custom.002 \
  --tag ghcr.io/luckykuang/sub2api-plus:v0.1.166-custom.002 \
  .
```

After that image is available to the Apple `container` runtime, set
`APPLE_CONTAINER_SUB2API_IMAGE=ghcr.io/luckykuang/sub2api-plus:v0.1.166-custom.002`. Until then, keep
the published image as the runtime base and use `APPLE_CONTAINER_SUB2API_BINARY`
for the custom binary.

The normal `up` command recreates the application container, so application environment changes are applied immediately. Use `up --recreate` when changing PostgreSQL, Redis, or MinIO container images or runtime configuration. Persistent data remains in named volumes.

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

The application container is intentionally recreated by every `up` and `restart` operation because dependency VM addresses can change after they stop. Application data remains in `sub2api-apple-data`.

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

./apple-container.sh pull
./apple-container.sh up --recreate
./apple-container.sh status
```

Database migrations are forward-only. Keep the previous image reference and both backups until the upgraded stack has been validated; image rollback alone cannot reverse a migrated database. Test restore procedures before relying on this workflow for important data.

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
