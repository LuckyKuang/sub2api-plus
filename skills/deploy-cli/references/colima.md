# Colima local deployment

Read [COLIMA.md](../../../deploy/COLIMA.md) for runtime preparation and the
shared [Docker deployment workflow](docker.md) for source selection, Compose
identity, replacement, readiness, recovery and retention.

1. Resolve the requested Colima profile (`default` unless selected otherwise).
   Read `colima --profile <profile> status --json` and require the Docker runtime.
   Its `docker_socket` must match the endpoint of `colima` for the default
   profile or `colima-<profile>` for a named profile. Require Docker, Compose and
   Buildx to be usable through that explicit context. Buildx inspection must
   show the `docker` driver with one running node on that context/socket;
   reject foreign or multi-node builders even if their name matches.
2. Reuse a running profile. If stopped, start it with `--activate=false` and the
   required mounts/resources. Repository validation needs at least four CPUs
   and ten GiB VM memory. Do not restart or resize a running profile whose
   other workloads would be interrupted without authorization.
3. Apply the shared Docker workflow with `docker --context <context>` on every
   operation, including Compose, pull/tag, health inspection and scoped image
   cleanup. Current-code builds use `--builder <context>`; Buildx inspection
   and cleanup use that same builder. Verify that the context endpoint still
   matches the selected Colima profile before any deployment mutation.
4. Check both the internal application endpoint and the configured published
   endpoint from macOS. Preserve persistent mounts and all source data when
   the requested target is a new Colima deployment.

Use the existing private environment file and established Compose project.
An old stack in a different runtime requires a separately planned data cutover;
changing these instructions does not migrate its volumes or authorize deleting
it. Runtime inspection projects only safe fields, never full environments,
resolved Compose configuration or unsanitized logs.
