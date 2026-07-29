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

## Environment Variables

| Variable | Description | Required | Default |
|----------|-------------|----------|---------|
| `DATABASE_URL` | PostgreSQL connection string | Yes | - |
| `REDIS_URL` | Redis connection string | Yes | - |
| `PORT` | Server port | No | `8080` |
| `GIN_MODE` | Gin framework mode (`debug`/`release`) | No | `release` |

## Supported Architectures

- `linux/amd64`
- `linux/arm64`

## Tags

- `latest` - Latest stable release
- `vX.Y.Z-custom.NNN` - Immutable fork release, for example `v0.1.166-custom.008`
- `x.y` - Latest patch of minor version
- `x` - Latest minor of major version

Git and GitHub Releases use `vX.Y.Z+custom.NNN`. The release workflow
preserves the leading `v` and replaces only `+` with `-` to produce the
OCI-compatible image tag. For example:

```text
Git/GitHub: v0.1.166+custom.008
GHCR:       ghcr.io/luckykuang/sub2api-plus:v0.1.166-custom.008
```

Pin the immutable release tag in production. Use `latest` only when automatic
movement to the newest custom release is intentional.

## Links

- [GitHub Repository](https://github.com/luckykuang/sub2api-plus)
- [Documentation](https://github.com/luckykuang/sub2api-plus#readme)
