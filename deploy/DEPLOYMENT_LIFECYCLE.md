# Local Deployment Lifecycle

This is the resource lifecycle contract for local deployments operated through
[`deploy-cli`](../skills/deploy-cli/SKILL.md). It supplements the existing
platform deployment commands; invoking an independent platform command does
not automatically sweep all historical development images.

## Runtime and ownership

Use Apple Containers on macOS, Docker inside a running WSL2 Debian/Ubuntu
distribution on Windows, and local Docker Engine with Compose v2 on Linux.
Builds and application checks run in containers; host processes orchestrate
the runtime and may probe the published application endpoint.

Scope operations to the selected deployment. Apple uses the owned
`sub2api-apple` Web container; Docker uses the selected Compose project's
`sub2api` service. Confirm ownership before replacement. Reuse the selected
environment file, project identity, storage mounts, and port bindings. Existing
development/production deployments are separate targets, not duplicates to
delete by name prefix.

Record the current Web container's immutable image identity before building,
pulling, or changing tags. Keep a private deployment record of source commit,
owned candidate/current/recovery image references and digests, and the last
healthy deployment. Keep this record in a private local directory outside the
repository, keyed by the selected target; do not record credentials or
environment contents. Serialize
mutations for the same target, including image preparation and cleanup. The
references' `local-candidate`/`local-current` examples use the default local
target; namespace these bounded aliases by a stable deployment identity when
operating multiple targets in one runtime. Do not overwrite another target's
alias.

Image names alone do not prove ownership. Adopt pre-existing historical images
only when the user has identified them for this deployment or their ownership
is established by an existing deployment record. Check every container,
including stopped containers and other projects, before deleting an image.
Compare immutable IDs/digests as well as reference aliases. If another target
still needs an image, retain it and report the reason.

## Replacement and recovery

Prepare and inspect the candidate before stopping the healthy Web service.
Local builds use the repository Dockerfile and version/commit metadata from
the selected source tree. Published-image deployments use the requested
version/digest; do not silently substitute `latest` for a requested version.

Keep the previous image addressable while a mutable current tag changes.
Remove the old Web container before creating the replacement with the same
persistent mounts. There is at most one managed Web container, including
stopped containers; replacement therefore has a short service interruption.
Database, Redis, and optional MinIO containers are separate dependency roles.
Reuse healthy dependencies during Web replacement.

Success requires a running Web container, a successful internal `/health`
probe, and a successful probe of its configured published endpoint. Bound
readiness using the selected platform's bounded probes. Only then update the
last-healthy deployment record and perform historical-image cleanup. A cleanup
failure leaves the healthy replacement running and is reported separately.

A preparation failure leaves the existing service in place. A replacement
failure preserves the previous image/configuration and recovery evidence.
Remove a failed replacement before recreating the previous compatible Web
container. Attempt recovery once and check its health; retain needed images
when recovery fails. Database migrations are forward-only: image recovery
does not reverse data changes. Follow the platform's backup guidance when
migration compatibility requires a restore; never automatically restore or
delete persistent data.

## Retention and cleanup

The successful steady state contains one Web container and one distinct
application-image digest by default. An explicitly requested rollback policy
may additionally retain one most recent successful predecessor. Candidates
and recovery images may coexist during an update; the retention limit applies
after success, not while preparing or recovering a deployment.

Remove obsolete owned application-image references after checking container
usage, including digest aliases. Remove an underlying image only when no
retained reference or container needs it. A stable `local-current` tag alone
does not reclaim old image content. Use bounded candidate/current/recovery
references rather than minting a permanent tag for every UI iteration.
Release tags in a registry remain immutable; local cleanup never publishes,
retags, or deletes registry artifacts.

Delete temporary deployment files and one-shot validation containers on
success or failure. Preserve recovery records/images needed after a failed
replacement. Persistent volumes, host data directories, credentials,
dependency images, `sub2api-validation` generations, and shared Builder or
BuildKit caches are outside application-image cleanup. Do not invoke global
container/image/volume/system/builder prune, force-delete running containers,
or force removal of in-use images. Shared builder cleanup is a separate
explicitly requested operation.

Report image ownership and usage exceptions instead of claiming that all
historical resources were removed. Use runtime disk usage and allocated-file
statistics as estimates; sparse files and APFS sharing affect reclaimed bytes.
