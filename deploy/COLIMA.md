# Colima deployment on macOS

`colima.sh` runs Sub2API Plus, PostgreSQL, Redis and optional MinIO in a dedicated
Colima Docker profile. It reuses `docker-compose.yml` with
`docker-compose.colima.yml`, including the application security settings and
environment bindings. Apple Containers remains a separate supported option.

## Requirements

- macOS supported by the installed Colima release.
- Colima, Docker CLI, Docker Compose 2.30.0 or newer, Python 3.10 or newer, Git
  for source builds, and curl for published-port readiness checks.
- At least 8 GiB available for the configured VM when building source.
- Network access to the configured image registries and build dependencies.

Verify CLI installation with `colima version` and `docker compose version`.
Docker Desktop is not required. An installed Docker CLI alone cannot list
images until the selected Colima Docker service is running.

## Quick start

From the repository root:

```bash
bash deploy/colima.sh init
# Review deploy/.env.colima.local; generated secrets must remain private.
bash deploy/colima.sh up
bash deploy/colima.sh status
```

Open `http://127.0.0.1:18080`. Use `ADMIN_EMAIL` and `ADMIN_PASSWORD` for a
preconfigured administrator, or retrieve the generated initial credentials
with `bash deploy/colima.sh logs app`. Service logs can contain sensitive data;
share only sanitized excerpts.

The default environment file is `deploy/.env.colima.local`, ignored by Git.
`SUB2API_ENV_FILE=/absolute/path/to/private.env` selects another file; pass it
consistently to every command. `init` refuses to overwrite an existing file,
generates independent random database, Redis, JWT, TOTP and MinIO secrets, and
sets mode `600`. Existing files must belong to the current user with no group
or other access. Values use literal `KEY=value` syntax, as in the Apple script:
do not add shell quoting, Compose interpolation or `export`. Duplicate keys are
rejected; initialization emits one definition per key from the shared template.

## Isolation and configuration

| Setting | Default | Purpose |
| --- | --- | --- |
| `COLIMA_PROFILE` | `sub2api` | Dedicated Docker VM |
| `COLIMA_PROJECT` | `sub2api-colima` | Container, volume, network and image namespace |
| `COLIMA_CPUS` | `4` | CPU allocation at VM startup |
| `COLIMA_MEMORY` | `8` | Memory in GiB at VM startup |
| `COLIMA_DISK` | `60` | VM disk capacity in GiB; not preallocated space |
| `COLIMA_DNS` | Empty | Optional comma-separated DNS server IPs at VM startup |
| `COLIMA_SUB2API_IMAGE` | `ghcr.io/luckykuang/sub2api-plus:latest` | Published image for initial deployment/upgrade; pin a tag or digest for repeatability |
| `COLIMA_RETAIN_PREVIOUS` | `false` | Retain one successful predecessor after an upgrade |
| `BIND_HOST` | `127.0.0.1` after init | Web publish address |
| `SERVER_PORT` | `18080` after init | Web published port; internal port remains `8080` |
| `MINIO_ENABLED` | `false` | Enable local image storage |
| `COLIMA_MINIO_IMAGE` | Shared `.env.example` preset | MinIO image; must include the `mc` client |
| `MINIO_API_PORT` / `MINIO_CONSOLE_PORT` | `19000` / `19001` after init | Loopback MinIO ports |

Other application and database settings come from `.env.example`. Colima
settings do not affect the Apple deployment. Profile/project names use lowercase
letters, digits, hyphens or underscores. Treat their identity and the selected
environment path as fixed for an existing target; changing them selects another
deployment rather than migrating data.

Every Docker operation includes the selected context (`colima-sub2api` by
default), and its endpoint must equal the profile's local Docker socket.
Colima starts with `--activate=false`; neither global Docker context nor the
existing `infoq-verify` profile is changed. Read-only commands do not start a
stopped VM. A pre-existing container/volume/network without the matching stack
and Compose project labels is rejected. Published ports are checked before Web
replacement, and target mutations are serialized by a private lock.

## Commands and image lifecycle

```bash
bash deploy/colima.sh up --build  # Build current source in Docker, then deploy
bash deploy/colima.sh build       # Prepare source candidate; leave Web running
bash deploy/colima.sh upgrade     # Pull configured application image, then replace Web
bash deploy/colima.sh status
bash deploy/colima.sh logs app -f
bash deploy/colima.sh restart
bash deploy/colima.sh down        # Stop services; preserve containers, data and VM
bash deploy/colima.sh disk-usage
```

Source builds use the root Dockerfile and inject `backend/cmd/server/VERSION`
and the Git commit. The deployment record also reports whether the worktree was
dirty. No Go, Node or frontend build runs on macOS itself. Plain `up` uses the
last successful image selection, so a later start cannot silently revert a
local build to the template's published image. Use `upgrade` to select the
configured published image or `up --build` to select current source.

Candidate preparation finishes before Web is stopped. Healthy dependencies
are reused with `--no-recreate`; dependency configuration changes therefore
require a separately planned restart/recreation. Web is explicitly stopped and
removed before its replacement is created. Readiness requires both a running
Web with an internal `/health` response and a successful published-port probe,
using a bounded 180-second retry budget and per-command timeouts.
`status` additionally requires healthy PostgreSQL, Redis and enabled MinIO.
`restart` ensures dependencies are running and restarts Web; `down` followed by
`up` performs a complete stack stop/start without discarding data.

The private target record lives under
`~/.local/state/sub2api/colima/<profile>/<project>/`. It contains environment
path, source metadata and image identities, never environment contents or
credentials. `SUB2API_COLIMA_STATE_DIR` can relocate the state root; preserve
the existing record when moving it. A leftover `operation.lock` deliberately
blocks mutations: inspect its PID and active operations before removing it.

After success, cleanup checks every container, including stopped containers,
and removes only recorded unused application images and this target's temporary
candidate/recovery aliases. Current images, requested predecessors, shared or
otherwise referenced images, persistent data, dependency images, validation
resources and shared build caches are preserved. No global prune or force
image removal is used. Retention exceptions are reported.

If image pulls fail with DNS errors, inspect the selected VM's resolver using
`colima --profile sub2api ssh -- cat /etc/resolv.conf`. Set `COLIMA_DNS` to DNS
servers reachable from this machine, stop only that profile with
`colima stop sub2api`, and run `up` again. Blank preserves Colima defaults;
the script never silently substitutes a public DNS service. Replace `sub2api`
with the actual selected profile in these commands.

If that file is a dangling symlink to a missing `systemd-resolved` stub, changing
`COLIMA_DNS` alone may not repair the guest image. This occurred with the local
Colima 0.10.3 Ubuntu 24.04 guest. Repair the resolver inside the selected VM or
update its base image; do not change another profile or application secrets.
For a confirmed dangling link, the following replaces it with a regular file;
use the machine's reachable DNS address instead of the example address:

```bash
colima --profile sub2api ssh -- sudo unlink /etc/resolv.conf
printf 'nameserver 192.0.2.53\n' | colima --profile sub2api ssh -- sudo tee /etc/resolv.conf
```

## MinIO and persistent data

Set `MINIO_ENABLED=true` in the private environment before first startup.
Initialization supplies MinIO credentials. The script starts MinIO, creates the
configured bucket, enables public downloads for that bucket (matching the Apple
deployment), and injects the internal endpoint and loopback public URL into
Sub2API. Keep this local storage scoped to test image results. Access from
another machine requires an appropriately reachable public URL and intentional
network configuration.

Application, PostgreSQL, Redis and MinIO use independent project-scoped named
volumes inside the Colima VM. Ordinary up/upgrade/restart/down preserve these
volumes. Do not use `docker compose down -v` or delete the Colima profile to
stop services; either can destroy persistent data. Back up PostgreSQL logically
and preserve the private environment, application and optional storage data.

Apple Containers volumes are not shared with Colima. A migration is a separate
backup/restore operation; do not bind the same live database directory into
both runtimes or assume copying its files constitutes a valid backup.

## Failure and recovery

A failed build or pull leaves the current Web running. Failed replacement
attempts one recovery with the previous immutable image and the selected
configuration, then still returns a deployment failure even if recovery passes.
If recovery fails, preserve the image and record; `recovery_needed` blocks further
deployment attempts until an operator resolves it. Image recovery does not
undo migrations and cannot guarantee compatibility with changed configuration
or a new schema. Use the application's backup guidance when restore is needed.
A cleanup failure leaves a healthy replacement in place and returns failure
for follow-up rather than rolling it back.

## Validation environment

Before macOS local validation ask which environment to use unless already
explicitly specified for the current scope. Preserve that choice; never select
from installed tools or automatically switch after failure. Repository checks
run inside explicitly selected Apple Containers or Colima using
[CONTRIBUTING.md](../CONTRIBUTING.md) and its maintained launcher. The Colima
deployment smoke check is separate from the full submission matrix and does
not create submission proof.
