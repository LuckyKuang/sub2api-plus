## Container and storage boundary

Every normal `up` deletes the old application container before creating the
replacement. This releases the old container VM filesystem and all unmounted
temporary files. The replacement still has an Apple Containers VM filesystem,
so the steady-state runtime footprint does not fall to the size of the files
visible inside the container.

`/app/storage` is deliberately outside this cleanup boundary. It is either an
owned named volume or a host bind mount and may contain logs, validated pricing
fallback files, configuration, and other application data. PostgreSQL, Redis,
and MinIO storage have the same persistence rule.

## Application image upgrade

The upgrade flow reads the image reference from the currently managed
application container. Before pulling the configured target image, it tags the
current image as `localhost/sub2api-apple-rollback:previous`. A stable tag
retains at most one script-managed rollback reference.

The flow pulls only the configured Sub2API image and invokes the normal
application redeployment path, leaving dependency containers untouched. Pull
or health-check failure leaves the rollback tag intact. When the operator
passes `--prune-previous-image`, deletion occurs only after the internal and
host-port application probes succeed. A same-digest pull removes the duplicate
rollback reference because it provides no rollback value.

Apple Container CLI 1.2.0 does not resolve image objects by the SHA-256 value
returned from `image inspect`. The design therefore tags the source reference
before pull instead of trying to rediscover an untagged digest afterward.

## Cleanup safety

`cleanup --dangling-images` calls the Apple Containers global dangling-image
prune only after explicit confirmation. It cannot be scoped by stack labels,
so it is never part of `up`, `pull`, or `upgrade`.

`cleanup --legacy-volumes` considers a named volume only when the corresponding
host data directory is configured. Before deleting anything, it verifies all
selected volumes are owned by this stack and that the persisted live container
configuration mounts the exact host source at the expected destination. A
missing container, mismatched mount, unowned resource, or reference to the
named volume from any Apple container aborts the operation before deletion.
Host directories are never cleanup targets.

## Failure behavior

All lifecycle operations retain the existing stack operation lock. Image and
volume discovery failures fail closed. Cleanup performs validation before
confirmation and deletion, and `--yes` skips only the prompt, never ownership
or mount verification.
