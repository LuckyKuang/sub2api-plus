---
name: deploy-cli
description: >-
  Build or pull, deploy, and verify Sub2API Plus on the current machine using
  Apple Containers on macOS, Docker inside WSL2 Debian/Ubuntu on Windows, or
  Docker on Linux. Use for requested local deployment, redeployment, runtime
  health checks, and scoped deployment-image cleanup. Repository test matrices,
  branch pushes, release publication, and admin API operations use their own
  workflows.
---

# Deploy CLI

Use this skill to operate a local deployment, not to publish an image or release.
Follow the shared [deployment lifecycle](../../deploy/DEPLOYMENT_LIFECYCLE.md).
An authorized local deployment includes replacing its Web container and
cleaning its owned obsolete application images under that policy. Creating or
reviewing this skill does not itself authorize operating a running deployment.

A status/health-only request performs inspection and probes without deploying
or cleaning resources. A cleanup-only request removes owned obsolete resources
under the retention policy without rebuilding or restarting the service. A
build-only request prepares the requested candidate without promoting it or
replacing the Web container; post-deployment retention applies after an actual
successful deployment.

## Runtime and target

- macOS: Apple Containers only; read [Apple Containers](references/apple-containers.md).
- Windows: Docker inside a running WSL2 Debian or Ubuntu distribution only;
  read [Docker and WSL2](references/docker.md). Never use Windows-host Docker.
- Linux: local Docker Engine and Compose v2; read [Docker and WSL2](references/docker.md).

Resolve the existing deployment directory, private environment file, container
ownership, persistent mounts, and current application image before changing
anything. Reuse established values. Ask only if the target or requested source
is genuinely ambiguous; do not guess a different deployment or recreate secrets.
Run builds and application checks in containers. Use the host only for Git
metadata, orchestration, runtime management, and published-port readiness probes.
Never fall back to a host Go/Node build or another platform runtime.

## Deployment workflow

1. Inspect the target and record safe resource fields: names, ownership, state,
   image references/digests, persistent mount paths, and disk usage. Preserve
   the existing deployment lock or acquire a lock for the selected target.
2. For current-code deployment, build the repository Dockerfile with the
   version from `backend/cmd/server/VERSION` and exact source commit. For a
   published-image deployment, pull the requested version or digest. Prepare
   the candidate while the existing Web service still runs.
3. Preserve the old image for recovery before moving a mutable tag. Reuse the
   deployment configuration and persistent mounts. Remove the old Web
   container before creating its replacement; do not create a second instance.
4. Verify both the application health endpoint inside its container and the
   configured published endpoint. Use bounded probes and the platform's
   readiness budget; do not declare success solely because the container is
   running.
5. After success, retain one Web container and one distinct application-image
   digest by default. If the user requests a rollback image, retain the most
   recent successful predecessor as well. Remove owned stale image references
   and temporary candidate/recovery references after checking all container
   references. Preserve data, dependencies, validation resources, and shared
   builders/caches.
6. On replacement failure, preserve recovery resources and restore the previous
   compatible image/configuration when possible. Attempt recovery once, verify
   it, and report the original failure plus the resulting service state. Do
   not undo database migrations or repeatedly cycle the service. A preparation
   failure leaves the old service running; a cleanup failure reports incomplete
   cleanup without rolling back a healthy replacement.

Report the deployed source/image digest, Web-container count, both health-check
results, retained application images, and cleanup outcome. Project safe fields
from runtime JSON; do not print full inspect output, environment files, resolved
Compose configuration, or unsanitized application logs.

Deployment verification is a runtime smoke check. Use `push-cli check` or
`submit-pr` separately when the repository validation matrix is requested;
deployment checks do not create its proof. Release publication stays with
`release-cli` and requires its own explicit request.
