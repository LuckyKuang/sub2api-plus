#!/bin/sh
set -eu

repo_root=$(CDPATH= cd -- "$(dirname -- "$0")/../.." && pwd)
cd "$repo_root"

# Parse templates only. Never load a deployment's private .env or contact its
# daemon. The pinned Compose binary is provided by Dockerfile.validation.
for compose_file in \
  deploy/docker-compose.yml \
  deploy/docker-compose.local.yml \
  deploy/docker-compose.standalone.yml \
  deploy/docker-compose.dev.yml
do
  DATABASE_HOST=validation-postgres.invalid DATABASE_PASSWORD=validation-only \
    REDIS_HOST=validation-redis.invalid \
    docker-compose --env-file deploy/.env.example -f "$compose_file" config --quiet
done
printf 'docker compose configuration test passed\n'
