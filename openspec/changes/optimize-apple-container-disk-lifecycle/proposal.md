## Why

Apple Containers gives every running container a separate lightweight VM
filesystem. Replacing the Sub2API application container already clears its
writable layer, but operators cannot distinguish that fixed runtime footprint
from dangling image snapshots or named volumes left behind after moving to
host bind mounts. The existing pull workflow also has no application-only,
health-gated rollback image lifecycle.

## What Changes

- Make the existing application-container replacement and persistent-storage
  boundary explicit in commands and documentation.
- Add an application-only image upgrade that preserves the current image under
  one stable rollback reference before pulling and deploying the target image.
- Allow the previous application image to be removed only after both
  application health probes succeed and the operator explicitly requests it.
- Add read-only Apple Containers disk usage reporting.
- Add explicit cleanup for global dangling images and for owned legacy named
  volumes that have been replaced by verified host bind mounts.
- Refuse legacy-volume deletion when ownership or the live mount source and
  destination cannot be proven.

## Capabilities

### New Capabilities

- `apple-container-disk-lifecycle`: Defines application writable-layer
  replacement, image rollback retention, disk reporting, and safe cleanup
  boundaries for the native Apple Containers deployment.

### Modified Capabilities

None.

## Impact

- **Deployment CLI**: Adds `upgrade`, `disk-usage`, and `cleanup` commands to
  `deploy/apple-container.sh`.
- **Persistent data**: Normal redeployment never clears application,
  PostgreSQL, Redis, MinIO, named-volume, or host bind data. Legacy named
  volumes can be deleted only by an explicit, verified cleanup operation.
- **Images**: Application upgrades retain one tagged rollback image by default.
  Global dangling-image cleanup remains explicit and warns that other Apple
  Containers projects may be affected.
- **Compatibility**: Existing `init`, `up`, `down`, `restart`, `status`, `logs`,
  `pull`, and `destroy` behavior remains available. No database migration,
  application configuration field, or release artifact changes are required.
