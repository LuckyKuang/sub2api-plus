# Sub2API Plus Docker Image

Sub2API Plus is an AI API Gateway Platform for distributing and managing AI product subscription API quotas.

## Quick Start

```bash
docker run -d \
  --name sub2api \
  -p 8080:8080 \
  -e DATABASE_URL="postgres://user:pass@host:5432/sub2api" \
  -e REDIS_URL="redis://host:6379" \
  ghcr.io/luckykuang/sub2api-plus:latest
```

## Docker Compose

For local deployment and verification through `skills/deploy-cli`, follow the
shared [deployment lifecycle](DEPLOYMENT_LIFECYCLE.md). It defines replacement,
health checks, recovery, and scoped application-image retention for both Linux
Docker and Docker inside WSL2 Debian/Ubuntu.

```yaml
version: '3.8'

services:
  sub2api:
    image: ghcr.io/luckykuang/sub2api-plus:latest
    ports:
      - "8080:8080"
    environment:
      - DATABASE_URL=postgres://postgres:postgres@db:5432/sub2api?sslmode=disable
      - REDIS_URL=redis://redis:6379
    depends_on:
      - db
      - redis

  db:
    image: postgres:15-alpine
    environment:
      - POSTGRES_USER=postgres
      - POSTGRES_PASSWORD=postgres
      - POSTGRES_DB=sub2api
    volumes:
      - postgres_data:/var/lib/postgresql/data

  redis:
    image: redis:7-alpine
    volumes:
      - redis_data:/data

volumes:
  postgres_data:
  redis_data:
```

## WSL2 Availability

If the published port stops responding after deployment, run
`wsl.exe --list --verbose` from PowerShell. WSL can stop an idle distribution
after the last attached command exits. Docker and the published port go offline
with the distribution. See [Microsoft's WSL service documentation](https://learn.microsoft.com/en-us/windows/wsl/systemd#how-does-enabling-systemd-affect-wsl-architecture).

For a twelve-hour audit window, keep this command open in a dedicated
PowerShell terminal. Substitute your deployment's distribution name for
`ubuntu`. Press Ctrl+C to end the session early.

```powershell
wsl.exe --distribution ubuntu --exec /usr/bin/sleep 12h
```

Once Docker is running, check `/health` on the configured published port again.

## Startup and Database Recovery

`concurrency.ping_interval` defaults to 10 seconds and accepts 0 or 5–30.
Setting it to 0 disables SSE keepalives while waiting for a slot, allowing a
later admission error to retain its HTTP status. A keepalive that has already
committed streaming headers requires subsequent errors to use the stream's
error framing. Configure this value in the mounted configuration file.

Sub2API runs database migrations while starting. PostgreSQL may still be
recovering briefly after a host or Docker daemon restart. The application
retries transient PostgreSQL startup and connection errors with bounded
exponential backoff, then continues startup when the database is ready.
Permanent errors such as invalid credentials, migration checksum mismatches,
SQL errors, and incompatible data fail immediately.

The Compose deployment also checks PostgreSQL readiness with both `pg_isready`
and a simple SQL query. `depends_on: condition: service_healthy` helps order a
fresh Compose start, but application-level retries are still required when
Docker restores existing containers after a host restart.

## Environment Variables

| Variable | Description | Required | Default |
|----------|-------------|----------|---------|
| `DATABASE_URL` | PostgreSQL connection string | Yes | - |
| `REDIS_URL` | Redis connection string | Yes | - |
| `PORT` | Server port | No | `8080` |
| `GIN_MODE` | Gin framework mode (`debug`/`release`) | No | `release` |
| `WEBAUTHN_ENABLED` | Enable the WebAuthn deployment boundary | No | `false` |
| `WEBAUTHN_RP_DISPLAY_NAME` | Relying-party display name | No | `Sub2API` |
| `WEBAUTHN_RP_ID` | Relying-party domain without scheme or port | When enabled | - |
| `WEBAUTHN_RP_ORIGINS` | Comma-separated allowed origins | When enabled | - |

Passkey remains disabled until the WebAuthn values are valid and an
administrator explicitly enables it in System Settings. Recreate the
application container after changing these values.

## Supported Architectures

- `linux/amd64`
- `linux/arm64`

## Tags

- `latest` - Latest stable release
- `vX.Y.Z-custom.NNN` - Immutable fork release, for example `v0.2.25-custom.001`
- `x.y` - Latest patch of minor version
- `x` - Latest minor of major version

Git and GitHub Releases use `vX.Y.Z+custom.NNN`. The release workflow
preserves the leading `v` and replaces only `+` with `-` to produce the
OCI-compatible image tag. For example:

```text
Git/GitHub: v0.2.25+custom.001
GHCR:       ghcr.io/luckykuang/sub2api-plus:v0.2.25-custom.001
```

Pin the immutable release tag in production. Use `latest` only when automatic
movement to the newest custom release is intentional.

## Links

- [GitHub Repository](https://github.com/luckykuang/sub2api-plus)
- [Documentation](https://github.com/luckykuang/sub2api-plus#readme)
