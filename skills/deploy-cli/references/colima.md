# Colima local deployment

Read [COLIMA.md](../../../deploy/COLIMA.md) for prerequisites, commands,
configuration, persistence and recovery, and follow the shared
[lifecycle contract](../../../deploy/DEPLOYMENT_LIFECYCLE.md).

Use the explicitly selected profile, project and private `SUB2API_ENV_FILE`;
the default is `deploy/.env.colima.local`. Do not adopt `infoq-verify`, an Apple
deployment or another Docker target. `colima.sh` verifies the local context,
ownership and target lock. It never changes the global Docker context.

For current-code deployment run `bash deploy/colima.sh up --build`; for the
configured published image run `bash deploy/colima.sh upgrade`. `build` prepares
a candidate without replacing Web. Plain `up` preserves the last successful
image selection. `status` checks both internal and published-port health.

Before macOS validation ask which environment to use unless already explicitly
specified for the current scope. A request to deploy with Colima specifies the
deployment smoke runtime, but does not implicitly choose a separate repository
validation runtime. Follow `CONTRIBUTING.md` for that selection and its explicit
launcher bindings; do not run regression tests on the host.

Preserve private configuration and volumes. Application replacement does not
recreate dependencies. Failed image preparation keeps Web running; failed
replacement attempts one recovery and reports failure even if recovery passes.
Retain recovery evidence when it fails; never reverse migrations. Cleanup is
scoped to recorded owned application images and checks stopped containers too.
