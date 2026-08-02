## ADDED Requirements

### Requirement: Application redeployment must replace only ephemeral container state

The Apple Containers deployment SHALL delete the existing managed Sub2API
application container before creating its replacement. It MUST preserve the
configured application data mount, dependency storage, and every host bind
directory.

#### Scenario: Redeploy a local application binary

- **WHEN** an operator rebuilds the configured local Sub2API binary and runs
  normal `up`
- **THEN** the old application container MUST be deleted before the replacement
  is created
- **THEN** PostgreSQL, Redis, and enabled MinIO containers MUST NOT be replaced
- **THEN** `/app/storage` and all dependency data MUST remain intact

### Requirement: Application image upgrade must preserve a usable rollback boundary

The deployment SHALL tag the image used by the current managed application
container before pulling a configured target image. It SHALL retain that
rollback reference when pull or deployment fails. It MUST NOT delete the
previous image before internal and host-port application health checks pass.

#### Scenario: Target image pull fails

- **WHEN** the current application image has been tagged for rollback and the
  target image pull fails
- **THEN** the upgrade MUST return a failure
- **THEN** the rollback image reference MUST remain available
- **THEN** persistent application and dependency data MUST remain unchanged

#### Scenario: Healthy upgrade requests previous image removal

- **WHEN** the new application image passes internal and host-port health checks
  and the operator explicitly requests previous-image pruning
- **THEN** the deployment SHALL remove the script-managed rollback reference
- **THEN** it MUST NOT remove an image reference still used by another
  container

### Requirement: Disk cleanup must be explicit and fail closed

The deployment SHALL expose read-only runtime disk reporting. Global dangling
image cleanup and legacy named-volume cleanup MUST require an explicit cleanup
selection and confirmation unless the operator supplies `--yes`.

#### Scenario: Global dangling-image cleanup

- **WHEN** the operator selects dangling-image cleanup
- **THEN** the deployment MUST warn that the Apple Containers image prune is
  global and may affect other projects
- **THEN** normal `up`, `pull`, and `upgrade` MUST NOT invoke global image prune

#### Scenario: Legacy named volume has a verified replacement bind mount

- **WHEN** an owned named volume exists, the corresponding host directory is
  configured, and the live managed container mounts that exact source at the
  expected destination
- **THEN** explicit legacy-volume cleanup MAY delete the named volume
- **THEN** it MUST NOT delete or clear the host bind directory

#### Scenario: Legacy named volume migration cannot be proven

- **WHEN** the corresponding container is missing, the mount source or
  destination differs, the volume is not owned by the stack, or any Apple
  container still references the named volume
- **THEN** cleanup MUST fail before deleting any selected legacy volume
